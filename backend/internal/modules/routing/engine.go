// Package routing picks provider targets for a virtual model and calls them with failover.
package routing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/settings"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

const (
	leaseGrace = 10 * time.Second
	sweepEvery = time.Second
	dialTimout = 10 * time.Second
)

// Consumer-defined views of the facades this package needs.
type (
	Models interface {
		ByName(ctx context.Context, name string) (virtualmodels.Model, error)
	}
	Providers interface {
		ByIDs(ctx context.Context, ids []string) (map[string]providers.Provider, error)
		Reveal(ctx context.Context, keyID string) (string, error)
	}
	Settings interface {
		Get(ctx context.Context) (settings.Settings, error)
	}
)

type Deps struct {
	Models    Models
	Providers Providers
	Settings  Settings
	Log       *slog.Logger
	Client    *http.Client     // optional
	Now       func() time.Time // optional
}

// Error carries the status and OpenAI-style error fields the gateway should answer with.
type Error struct {
	Status     int
	Code       string
	Message    string
	RetryAfter int // seconds, 0 = none
}

func (e *Error) Error() string { return e.Message }

// Result is an upstream response passed through as is: buffered in Body, or live in Stream.
// A non-nil Stream holds the key lease until it is closed.
type Result struct {
	Status      int
	ContentType string
	Body        []byte
	Stream      io.ReadCloser
}

type Engine struct {
	models    Models
	providers Providers
	settings  Settings
	log       *slog.Logger
	client    *http.Client
	now       func() time.Time

	mu       sync.Mutex
	inflight map[string]int // by provider key id
	leases   map[uint64]lease
	lastID   uint64
	cooldown map[string]time.Time // by provider key id
	changed  chan struct{}        // closed and replaced when capacity may have appeared
	cursor   map[string]int       // round robin, by tier
	credit   map[string]int       // smooth weighted round robin current weights
}

func New(d Deps) *Engine {
	e := &Engine{
		models: d.Models, providers: d.Providers, settings: d.Settings, log: d.Log, client: d.Client, now: d.Now,
		inflight: map[string]int{}, leases: map[uint64]lease{}, cooldown: map[string]time.Time{},
		changed: make(chan struct{}), cursor: map[string]int{}, credit: map[string]int{},
	}
	if e.log == nil {
		e.log = slog.Default()
	}
	if e.now == nil {
		e.now = time.Now
	}
	if e.client == nil {
		e.client = &http.Client{Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: dialTimout}).DialContext,
			TLSHandshakeTimeout: dialTimout,
			MaxIdleConnsPerHost: 32,
			IdleConnTimeout:     90 * time.Second,
		}}
	}
	return e
}

// Do routes a chat completion body to the virtual model's targets and returns the upstream answer.
// Errors are *Error, or the context error when the caller went away.
func (e *Engine) Do(ctx context.Context, name string, hdr http.Header, body map[string]json.RawMessage) (Result, error) {
	return e.route(ctx, name, hdr, chatPayload(body), false)
}

// Transcribe routes a buffered multipart transcription request; never streams.
func (e *Engine) Transcribe(ctx context.Context, name string, hdr http.Header, parts []Part) (Result, error) {
	return e.route(ctx, name, hdr, formPayload(parts), false)
}

// Stream is Do for streaming bodies: a 2xx answer comes back as Result.Stream, committed to that target.
// Callers must close it; read errors are *Error (timeout) or the context error.
func (e *Engine) Stream(ctx context.Context, name string, hdr http.Header, body map[string]json.RawMessage) (Result, error) {
	return e.route(ctx, name, hdr, chatPayload(body), true)
}

// Messages routes a native Anthropic messages body to anthropic targets, byte for byte except the model.
func (e *Engine) Messages(ctx context.Context, name string, hdr http.Header, body map[string]json.RawMessage) (Result, error) {
	return e.route(ctx, name, hdr, messagesPayload(body), false)
}

// MessagesStream is Messages for streaming bodies, committed like Stream.
func (e *Engine) MessagesStream(ctx context.Context, name string, hdr http.Header, body map[string]json.RawMessage) (Result, error) {
	return e.route(ctx, name, hdr, messagesPayload(body), true)
}

func (e *Engine) route(ctx context.Context, name string, hdr http.Header, pl payload, stream bool) (res Result, err error) {
	cfg, err := e.settings.Get(ctx)
	if err != nil {
		return Result{}, err
	}
	timeout := time.Duration(cfg.RequestTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer func() {
		if res.Stream == nil {
			cancel()
		}
	}()

	m, err := e.models.ByName(ctx, name)
	switch {
	case errors.Is(err, virtualmodels.ErrNotFound):
		return Result{}, &Error{Status: http.StatusNotFound, Code: "model_not_found", Message: "model " + name + " does not exist"}
	case err != nil:
		return Result{}, err
	case m.Kind != pl.kind:
		return Result{}, &Error{Status: http.StatusBadRequest, Code: "wrong_model_kind", Message: "model " + name + " is not a " + string(pl.kind) + " model"}
	}
	chain, err := e.chain(ctx, m, pl)
	if err != nil {
		return Result{}, err
	}
	r := &request{
		e: e, ctx: ctx, cancel: cancel, cfg: cfg, model: m, chain: chain, hdr: hdr, pl: pl, stream: stream,
		ttl: timeout + leaseGrace, tried: map[string]bool{},
	}
	return r.run()
}
