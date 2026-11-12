package routing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const maxCooldown = 24 * time.Hour

// call sends the request upstream and always releases the lease. A non-nil error means try the next target.
func (r *request) call(a attempt) (Result, error) {
	defer r.e.release(a.lease)
	name := a.t.provider.Name
	secret, err := r.e.providers.Reveal(r.ctx, a.key.ID)
	if err != nil {
		r.e.log.Error("reveal provider key", "provider", name, "key", a.key.ID, "err", err)
		return Result{}, fmt.Errorf("provider %q: key unavailable", name)
	}
	req, err := http.NewRequestWithContext(r.ctx, http.MethodPost,
		strings.TrimRight(a.t.provider.BaseURL, "/")+"/chat/completions", bytes.NewReader(withModel(r.body, a.t.entry.Model)))
	if err != nil {
		return Result{}, fmt.Errorf("provider %q: bad base URL", name)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)

	resp, err := r.e.client.Do(req)
	if err != nil {
		r.e.log.Warn("upstream request failed", "provider", name, "err", err)
		return Result{}, fmt.Errorf("provider %q: request failed", name)
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
