// Package gateway serves the OpenAI-compatible /v1 API behind proxy-key auth.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/proxykeys"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/routing"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/settings"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

const (
	touchTimeout  = 5 * time.Second
	touchInterval = 30 * time.Second
)

// Consumer-defined views of the facades and engine this package needs.
type (
	Keys interface {
		Verify(ctx context.Context, token string) (proxykeys.Key, error)
		Touch(ctx context.Context, id string) error
	}
	Models interface {
		List(ctx context.Context) ([]virtualmodels.Model, error)
	}
	Chat interface {
		Do(ctx context.Context, name string, hdr http.Header, body map[string]json.RawMessage) (routing.Result, error)
		Stream(ctx context.Context, name string, hdr http.Header, body map[string]json.RawMessage) (routing.Result, error)
	}
	Settings interface {
		Get(ctx context.Context) (settings.Settings, error)
	}
)

type Deps struct {
	Keys     Keys
	Models   Models
	Chat     Chat
	Settings Settings
	Log      *slog.Logger
}

type gateway struct {
	Deps
	mu      sync.Mutex
	touched map[string]time.Time
}

type keyCtx struct{}

// New returns the handler for everything under /v1 (mount it at that prefix).
func New(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	g := &gateway{Deps: d, touched: map[string]time.Time{}}
	r := chi.NewRouter()
	r.Use(g.auth)
	r.Get("/models", g.models)
	r.Post("/chat/completions", g.chatCompletions)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown endpoint", 0)
	})
	return r
}

func (g *gateway) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("x-api-key")
		if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			token = bearer
		}
		if token = strings.TrimSpace(token); token == "" {
			writeError(w, http.StatusUnauthorized, "missing_api_key", "missing API key", 0)
			return
		}
		key, err := g.Keys.Verify(r.Context(), token)
		switch {
		case errors.Is(err, proxykeys.ErrInvalidKey):
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "invalid API key", 0)
			return
		case err != nil:
			g.Log.ErrorContext(r.Context(), "verify proxy key", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal error", 0)
			return
		}
		if g.shouldTouch(key.ID) {
			go g.touch(context.WithoutCancel(r.Context()), key.ID)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyCtx{}, key)))
	})
}

// shouldTouch debounces last-used writes: SQLite has one writer.
func (g *gateway) shouldTouch(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if time.Since(g.touched[id]) < touchInterval {
		return false
	}
	g.touched[id] = time.Now()
	return true
}

func (g *gateway) touch(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(ctx, touchTimeout)
	defer cancel()
	if err := g.Keys.Touch(ctx, id); err != nil {
		g.Log.Warn("touch proxy key", "key", id, "err", err)
	}
}

func (g *gateway) models(w http.ResponseWriter, r *http.Request) {
	key := r.Context().Value(keyCtx{}).(proxykeys.Key)
	all, err := g.Models.List(r.Context())
	if err != nil {
		g.fail(w, r, err)
		return
	}
	data := []map[string]any{}
	for _, m := range all {
		if key.AllowsModel(m.Name) {
			data = append(data, map[string]any{"id": m.Name, "object": "model", "created": m.CreatedAt.Unix(), "owned_by": "ai-proxy"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (g *gateway) chatCompletions(w http.ResponseWriter, r *http.Request) {
	cfg, err := g.Settings.Get(r.Context())
	if err != nil {
		g.fail(w, r, err)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(cfg.MaxBodyBytes)))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large", 0)
		return
	}
	var body map[string]json.RawMessage
	var model string
	if err != nil || json.Unmarshal(raw, &body) != nil || json.Unmarshal(body["model"], &model) != nil || model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "body must be a JSON object with a model", 0)
		return
	}
	var stream bool
	_ = json.Unmarshal(body["stream"], &stream)
	switch {
	case !r.Context().Value(keyCtx{}).(proxykeys.Key).AllowsModel(model):
		writeError(w, http.StatusForbidden, "model_not_allowed", "this key may not use model "+model, 0)
	default:
		g.forward(w, r, model, body, stream)
	}
}

func (g *gateway) forward(w http.ResponseWriter, r *http.Request, model string, body map[string]json.RawMessage, stream bool) {
	call := g.Chat.Do
	if stream {
		call = g.Chat.Stream
	}
	res, err := call(r.Context(), model, r.Header, body)
	if err != nil {
		g.fail(w, r, err)
		return
	}
	if res.ContentType == "" {
		res.ContentType = "application/json"
	}
	w.Header().Set("Content-Type", res.ContentType)
	if res.Stream != nil {
		g.pipe(w, r, res)
		return
	}
	w.WriteHeader(res.Status)
	_, _ = w.Write(res.Body)
}

// pipe commits to the upstream stream: headers out, then bytes flushed as they arrive; no failover from here.
func (g *gateway) pipe(w http.ResponseWriter, r *http.Request, res routing.Result) {
	defer func() { _ = res.Stream.Close() }()
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(res.Status)
	rc := http.NewResponseController(w)
	_ = rc.Flush()
	err := copyFlushing(w, rc, res.Stream)
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	g.Log.WarnContext(r.Context(), "upstream stream broke", "err", err)
	code, msg := "stream_interrupted", "upstream stream interrupted"
	var re *routing.Error
	if errors.As(err, &re) {
		code, msg = re.Code, re.Message
	}
	event, _ := json.Marshal(map[string]any{"error": map[string]string{"message": msg, "type": "upstream_error", "code": code}})
	_, _ = w.Write(append(append([]byte("data: "), event...), "\n\n"...))
	_ = rc.Flush()
}

// copyFlushing returns the read error that ended the copy; a client write failure counts as cancelled.
func copyFlushing(w io.Writer, rc *http.ResponseController, src io.Reader) error {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return context.Canceled
			}
			_ = rc.Flush()
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// fail maps engine errors to their status; a vanished client gets nothing, anything else is a 500.
func (g *gateway) fail(w http.ResponseWriter, r *http.Request, err error) {
	var re *routing.Error
	switch {
	case errors.As(err, &re):
		writeError(w, re.Status, re.Code, re.Message, re.RetryAfter)
	case errors.Is(err, context.Canceled):
	default:
		g.Log.ErrorContext(r.Context(), "gateway request", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error", 0)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, retryAfter int) {
	typ := "invalid_request_error"
	switch {
	case status == http.StatusUnauthorized:
		typ = "authentication_error"
	case status == http.StatusTooManyRequests:
		typ = "rate_limit_error"
	case status >= http.StatusInternalServerError:
		typ = "api_error"
	}
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": msg, "type": typ, "code": code}})
}
