package routing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
)

const (
	maxCooldown             = 24 * time.Hour
	defaultAnthropicVersion = "2023-06-01"
)

// call sends the request upstream and releases the lease unless a stream takes it over.
// A non-nil error means try the next target.
func (r *request) call(a attempt) (Result, error) {
	handedOff := false
	defer func() {
		if !handedOff {
			r.e.release(a.lease)
		}
	}()
	name := a.t.provider.Name
	secret, err := r.e.providers.Reveal(r.ctx, a.key.ID)
	if err != nil {
		r.e.log.Error("reveal provider key", "provider", name, "key", a.key.ID, "err", err)
		return Result{}, fmt.Errorf("provider %q: key unavailable", name)
	}
	req, err := r.upstreamRequest(a, secret)
	if err != nil {
		return Result{}, fmt.Errorf("provider %q: bad base URL", name)
	}
	resp, err := r.e.client.Do(req)
	if err != nil {
		r.e.log.Warn("upstream request failed", "provider", name, "err", err)
		return Result{}, fmt.Errorf("provider %q: request failed", name)
	}
	if r.stream && resp.StatusCode/100 == 2 {
		handedOff = true
		return Result{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Stream: &liveBody{ReadCloser: resp.Body, r: r, lease: a.lease}}, nil
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		r.e.log.Warn("upstream body read failed", "provider", name, "err", err)
		return Result{}, fmt.Errorf("provider %q: response interrupted", name)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		d := r.cooldownFor(resp.Header.Get("Retry-After"))
		r.e.cool(a.key.ID, d)
		return Result{}, fmt.Errorf("provider %q: rate limited, key paused %ds", name, int(d.Seconds()))
	case resp.StatusCode >= http.StatusInternalServerError:
		return Result{}, fmt.Errorf("provider %q: upstream status %d", name, resp.StatusCode)
	}
	return Result{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: data}, nil
}

// cooldownFor reads Retry-After (seconds or HTTP date), else the configured default.
func (r *request) cooldownFor(header string) time.Duration {
	d := time.Duration(r.cfg.DefaultCooldownSeconds) * time.Second
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		d = time.Duration(min(secs, int(maxCooldown/time.Second))) * time.Second
	} else if t, err := http.ParseTime(header); err == nil && t.After(r.e.now()) {
		d = t.Sub(r.e.now())
	}
	return min(d, maxCooldown)
}

// withModel re-encodes the body with only "model" replaced; every other value stays byte for byte.
func withModel(body map[string]json.RawMessage, model string) []byte {
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(k)
		b.Write(name)
		b.WriteByte(':')
		if k == "model" {
			v, _ := json.Marshal(model)
			b.Write(v)
		} else {
			b.Write(body[k])
		}
	}
	b.WriteByte('}')
	return b.Bytes()
}

func (r *request) upstreamRequest(a attempt, secret string) (*http.Request, error) {
	body, contentType := r.pl.build(a.t.entry.Model)
	req, err := http.NewRequestWithContext(r.ctx, http.MethodPost, upstreamURL(a.t.provider, r.pl.path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	forwardHeaders(req.Header, r.hdr)
	req.Header.Set("Content-Type", contentType)
	switch a.t.provider.Kind {
	case providers.KindOpenAICompatible:
		req.Header.Set("Authorization", "Bearer "+secret)
	case providers.KindAnthropic:
		req.Header.Set("X-Api-Key", secret)
		if req.Header.Get("Anthropic-Version") == "" {
			req.Header.Set("Anthropic-Version", defaultAnthropicVersion)
		}
	}
	return req, nil
}

// upstreamURL joins base and path; Anthropic bases may omit the /v1 prefix.
func upstreamURL(p providers.Provider, path string) string {
	base := strings.TrimRight(p.BaseURL, "/")
	if p.Kind == providers.KindAnthropic && !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + path
}

// liveBody is a committed upstream stream; closing it frees the key lease and the request deadline.
type liveBody struct {
	io.ReadCloser
	r     *request
	lease uint64
	once  sync.Once
}

// Read reports the request's own ctx error, so callers can tell a deadline or client exit from an upstream break.
func (b *liveBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		if cerr := b.r.ctx.Err(); cerr != nil {
			err = ctxError(cerr)
		}
	}
	return n, err
}

func (b *liveBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() {
		b.r.cancel()
		b.r.e.release(b.lease)
	})
	return err
}
