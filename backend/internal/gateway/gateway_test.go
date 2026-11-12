package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/proxykeys"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/routing"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/settings"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/httpx"
)

var keySeq atomic.Int32

type fakeModels map[string]virtualmodels.Model

func (f fakeModels) ByName(_ context.Context, name string) (virtualmodels.Model, error) {
	if m, ok := f[name]; ok {
		return m, nil
	}
	return virtualmodels.Model{}, virtualmodels.ErrNotFound
}

func (f fakeModels) List(context.Context) ([]virtualmodels.Model, error) {
	var out []virtualmodels.Model
	for _, m := range f {
		out = append(out, m)
	}
	return out, nil
}

type fakeProviders map[string]providers.Provider

func (f fakeProviders) ByIDs(_ context.Context, ids []string) (map[string]providers.Provider, error) {
	out := map[string]providers.Provider{}
	for _, id := range ids {
		if p, ok := f[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

func (f fakeProviders) Reveal(_ context.Context, keyID string) (string, error) {
	return "real-" + keyID, nil
}

type fakeSettings struct{ s settings.Settings }

func (f *fakeSettings) Get(context.Context) (settings.Settings, error) { return f.s, nil }

type env struct {
	srv      *httptest.Server
	keys     *proxykeys.Service
	token    string
	models   fakeModels
	provs    fakeProviders
	settings *fakeSettings
}

func newEnv(t *testing.T) *env {
	t.Helper()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	e := &env{
		keys: proxykeys.New(proxykeys.Deps{DB: client}), models: fakeModels{}, provs: fakeProviders{},
		settings: &fakeSettings{s: settings.Settings{MaxBodyBytes: 1 << 20, RequestTimeoutSeconds: 30, QueueWaitSeconds: 1, DefaultCooldownSeconds: 60}},
	}
	engine := routing.New(routing.Deps{Models: e.models, Providers: e.provs, Settings: e.settings})
	h := New(Deps{Keys: e.keys, Models: e.models, Chat: engine, Settings: e.settings})
	router := httpx.NewRouter(httpx.Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	router.Mount("/v1", h)
	e.srv = httptest.NewServer(router)
	t.Cleanup(e.srv.Close)
	_, e.token = e.newKey(t)
	return e
}

func (e *env) newKey(t *testing.T, allowed ...string) (proxykeys.Key, string) {
	t.Helper()
	k, secret, err := e.keys.Create(context.Background(), proxykeys.CreateInput{Name: fmt.Sprint("k", keySeq.Add(1)), AllowedModels: allowed})
	if err != nil {
		t.Fatal(err)
	}
	return k, secret
}

// upstream registers a fake provider with one key (limit, weight 1) and returns its call counter.
func (e *env) upstream(t *testing.T, id string, limit int, h http.HandlerFunc) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	e.provs[id] = providers.Provider{
		ID: id, Name: id, Kind: providers.KindOpenAICompatible, Enabled: true, BaseURL: srv.URL + "/v1",
		Keys: []providers.Key{{ID: "k-" + id, Enabled: true, MaxInFlight: limit, Weight: 1}},
	}
	return &hits
}

// route defines model "m" whose tiers hold one entry per listed provider id, tried in order (priority_overflow).
func (e *env) route(tiers ...[]string) {
	m := virtualmodels.Model{Name: "m", Kind: virtualmodels.KindChat, Strategy: virtualmodels.StrategyPriorityOverflow}
	for i, ids := range tiers {
		tier := virtualmodels.Tier{Priority: i}
		for _, id := range ids {
			tier.Entries = append(tier.Entries, virtualmodels.Entry{ID: "e-" + id, ProviderID: id, Model: "real-" + id, Weight: 1, Status: virtualmodels.StatusOK})
		}
		m.Tiers = append(m.Tiers, tier)
	}
	e.models["m"] = m
}

func ok(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func status(code int, header ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(header); i += 2 {
			w.Header().Set(header[i], header[i+1])
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":"upstream says no"}`))
	}
}

type reply struct {
	StatusCode int
	Header     http.Header
}

func (e *env) do(t *testing.T, method, path, body string, headers map[string]string) (reply, string) {
	t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header}, string(data)
}

func (e *env) chat(t *testing.T, body string) (reply, string) {
	t.Helper()
	return e.do(t, http.MethodPost, "/v1/chat/completions", body, map[string]string{"Authorization": "Bearer " + e.token})
}

const simple = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`

func wantError(t *testing.T, resp reply, body string, status int, code string) {
	t.Helper()
	var out struct {
		Error struct{ Message, Type, Code string }
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || resp.StatusCode != status || out.Error.Code != code || out.Error.Type == "" || out.Error.Message == "" {
		t.Fatalf("want %d %s, got %d %s", status, code, resp.StatusCode, body)
	}
}

func TestChatRewritesModelAndKeyOnly(t *testing.T) {
	e := newEnv(t)
	var gotBody []byte
	var gotAuth, gotPath, gotType string
	e.upstream(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotAuth, gotPath, gotType = r.Header.Get("Authorization"), r.URL.Path, r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json; x=1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"up"}`))
	})
	e.route([]string{"a"})
	image := `[{"type":"text","text":"a <b> & c"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]`
	resp, body := e.chat(t, `{ "model" : "m", "messages":[{"role":"user","content":`+image+`}], "temperature": 0.5 }`)

	if resp.StatusCode != http.StatusCreated || body != `{"id":"up"}` || resp.Header.Get("Content-Type") != "application/json; x=1" {
		t.Fatalf("response not passed through: %d %q %q", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	if gotAuth != "Bearer real-k-a" || gotPath != "/v1/chat/completions" || gotType != "application/json" {
		t.Fatalf("upstream saw auth=%q path=%q type=%q", gotAuth, gotPath, gotType)
	}
	var seen map[string]json.RawMessage
	if err := json.Unmarshal(gotBody, &seen); err != nil {
		t.Fatal(err)
	}
	if string(seen["model"]) != `"real-a"` || string(seen["temperature"]) != `0.5` || !bytes.Contains(seen["messages"], []byte(image)) {
		t.Fatalf("body not preserved: %s", gotBody)
	}
}

func TestFailoverToNextTarget(t *testing.T) {
	broken := map[string]http.HandlerFunc{
		"5xx":   status(http.StatusBadGateway),
		"reset": func(w http.ResponseWriter, _ *http.Request) { c, _, _ := w.(http.Hijacker).Hijack(); _ = c.Close() },
	}
	for name, h := range broken {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			a := e.upstream(t, "a", 1, h)
			e.upstream(t, "b", 1, ok(`{"from":"b"}`))
			e.route([]string{"a", "b"})
			for range 2 {
				if resp, body := e.chat(t, simple); resp.StatusCode != 200 || body != `{"from":"b"}` {
					t.Fatalf("%d %s", resp.StatusCode, body)
				}
			}
			if a.Load() != 2 {
				t.Fatalf("a was hit %d times, want 2: failures must not cool the key", a.Load())
			}
		})
	}
	t.Run("connection refused", func(t *testing.T) {
		e := newEnv(t)
		e.upstream(t, "a", 1, ok("{}"))
		dead := httptest.NewServer(nil)
		dead.Close()
		p := e.provs["a"]
		p.BaseURL = dead.URL + "/v1"
		e.provs["a"] = p
		e.upstream(t, "b", 1, ok(`{"from":"b"}`))
		e.route([]string{"a", "b"})
		if _, body := e.chat(t, simple); body != `{"from":"b"}` {
			t.Fatal(body)
		}
	})
}

func TestRateLimitedKeyIsCooledAndSkipped(t *testing.T) {
	for name, retryAfter := range map[string][]string{"retry-after": {"Retry-After", "120"}, "default cooldown": nil} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			a := e.upstream(t, "a", 1, status(http.StatusTooManyRequests, retryAfter...))
			e.upstream(t, "b", 1, ok(`{"from":"b"}`))
			e.route([]string{"a", "b"})
			for range 3 {
				if _, body := e.chat(t, simple); body != `{"from":"b"}` {
					t.Fatal(body)
				}
			}
			if a.Load() != 1 {
				t.Fatalf("cooling key was called %d times, want 1", a.Load())
			}
		})
	}
}

func TestClientErrorsPassThrough(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest} {
		e := newEnv(t)
		e.upstream(t, "a", 1, status(code))
		b := e.upstream(t, "b", 1, ok("{}"))
		e.route([]string{"a", "b"})
		if resp, body := e.chat(t, simple); resp.StatusCode != code || body != `{"error":"upstream says no"}` || b.Load() != 0 {
			t.Fatalf("%d: got %d %s, b hit %d", code, resp.StatusCode, body, b.Load())
		}
	}
}

func TestAllTargetsDownIs502WithLastFailure(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, status(http.StatusInternalServerError))
	e.upstream(t, "b", 1, status(http.StatusServiceUnavailable))
	e.route([]string{"a"}, []string{"b"})
	resp, body := e.chat(t, simple)
	wantError(t, resp, body, http.StatusBadGateway, "upstream_error")
	if !strings.Contains(body, "status 503") {
		t.Fatalf("last failure missing: %s", body)
	}
}

func TestNoUsableTargetsIs503Immediately(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, ok("{}"))
	p := e.provs["a"]
	p.Enabled = false
	e.provs["a"] = p
	e.route([]string{"a"})
	start := time.Now()
	resp, body := e.chat(t, simple)
	wantError(t, resp, body, http.StatusServiceUnavailable, "no_available_provider")
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("503 waited")
	}
}

// blocker is an upstream that holds every request until released; it tracks peak concurrency.
type blocker struct {
	release     chan struct{}
	cur, peak   atomic.Int32
	arrived     chan struct{}
	handlerFunc http.HandlerFunc
}

func newBlocker() *blocker {
	b := &blocker{release: make(chan struct{}), arrived: make(chan struct{}, 16)}
	b.handlerFunc = func(w http.ResponseWriter, r *http.Request) {
		n := b.cur.Add(1)
		for p := b.peak.Load(); n > p && !b.peak.CompareAndSwap(p, n); p = b.peak.Load() {
		}
		b.arrived <- struct{}{}
		defer b.cur.Add(-1)
		select {
		case <-b.release:
			ok(`{"ok":true}`)(w, r)
		case <-r.Context().Done():
		}
	}
	return b
}

type answer struct {
	resp reply
	body string
}

func (e *env) chatAsync(t *testing.T) <-chan answer {
	ch := make(chan answer, 1)
	go func() {
		resp, body := e.chat(t, simple)
		ch <- answer{resp, body}
	}()
	return ch
}

func TestKeyLimitQueuesExcessRequests(t *testing.T) {
	e := newEnv(t)
	e.settings.s.QueueWaitSeconds = 10
	b := newBlocker()
	e.upstream(t, "a", 2, b.handlerFunc)
	e.route([]string{"a"})
	answers := []<-chan answer{e.chatAsync(t), e.chatAsync(t), e.chatAsync(t)}
	<-b.arrived
	<-b.arrived
	select {
	case <-b.arrived:
		t.Fatal("third request reached the upstream over limit 2")
	case <-time.After(300 * time.Millisecond):
	}
	close(b.release)
	for _, ch := range answers {
		if a := <-ch; a.resp.StatusCode != 200 {
			t.Fatalf("%d %s", a.resp.StatusCode, a.body)
		}
	}
	if b.peak.Load() != 2 {
		t.Fatalf("peak concurrency %d", b.peak.Load())
	}
}

func TestFullChainWaitsThenAnswers429(t *testing.T) {
	e := newEnv(t)
	b := newBlocker()
	e.upstream(t, "a", 1, b.handlerFunc)
	e.route([]string{"a"})
	first := e.chatAsync(t)
	<-b.arrived
	start := time.Now()
	resp, body := e.chat(t, simple)
	wantError(t, resp, body, http.StatusTooManyRequests, "rate_limit_exceeded")
	if resp.Header.Get("Retry-After") != "1" || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("retry-after %q after %v", resp.Header.Get("Retry-After"), time.Since(start))
	}
	close(b.release)
	if a := <-first; a.resp.StatusCode != 200 {
		t.Fatal(a.body)
	}
}

func TestCancelledWaiterLeaksNothing(t *testing.T) {
	e := newEnv(t)
	e.settings.s.QueueWaitSeconds = 30
	b := newBlocker()
	e.upstream(t, "a", 1, b.handlerFunc)
	e.route([]string{"a"})
	first := e.chatAsync(t)
	<-b.arrived

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.srv.URL+"/v1/chat/completions", strings.NewReader(simple))
	req.Header.Set("Authorization", "Bearer "+e.token)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
		t.Fatal("waiting request should have been cancelled")
	}
	close(b.release)
	<-first
	if a := <-e.chatAsync(t); a.resp.StatusCode != 200 {
		t.Fatalf("lease leaked: %d %s", a.resp.StatusCode, a.body)
	}
}

func TestRequestValidation(t *testing.T) {
	e := newEnv(t)
	e.settings.s.MaxBodyBytes = 200
	e.upstream(t, "a", 1, ok("{}"))
	e.route([]string{"a"})
	e.models["audio"] = virtualmodels.Model{Name: "audio", Kind: virtualmodels.KindTranscription}
	for name, tc := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"too large":     {`{"model":"m","pad":"` + strings.Repeat("x", 300) + `"}`, 413, "request_too_large"},
		"unknown model": {`{"model":"nope"}`, 404, "model_not_found"},
		"wrong kind":    {`{"model":"audio"}`, 400, "wrong_model_kind"},
		"no model":      {`{"messages":[]}`, 400, "invalid_request"},
		"not json":      {`nope`, 400, "invalid_request"},
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := e.chat(t, tc.body)
			wantError(t, resp, body, tc.status, tc.code)
		})
	}
	if resp, _ := e.chat(t, `{"model":"m","stream":false}`); resp.StatusCode != 200 {
		t.Fatalf("stream:false rejected: %d", resp.StatusCode)
	}
}

func TestAuthMatrix(t *testing.T) {
	e := newEnv(t)
	revoked, revokedToken := e.newKey(t)
	if _, err := e.keys.Revoke(context.Background(), revoked.ID); err != nil {
		t.Fatal(err)
	}
	_, unknown := e.newKey(t)
	unknown = unknown[:len(unknown)-2] + "AA"
	for name, tc := range map[string]struct {
		headers map[string]string
		status  int
	}{
		"bearer":       {map[string]string{"Authorization": "Bearer " + e.token}, 200},
		"x-api-key":    {map[string]string{"x-api-key": e.token}, 200},
		"revoked":      {map[string]string{"Authorization": "Bearer " + revokedToken}, 401},
		"unknown":      {map[string]string{"Authorization": "Bearer " + unknown}, 401},
		"empty":        {map[string]string{"Authorization": "Bearer "}, 401},
		"none":         {nil, 401},
		"malformed":    {map[string]string{"Authorization": "Bearer sk-apx-short"}, 401},
		"wrong prefix": {map[string]string{"x-api-key": "sk-other-xxxx"}, 401},
		"basic scheme": {map[string]string{"Authorization": "Basic " + e.token}, 401},
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := e.do(t, http.MethodGet, "/v1/models", "", tc.headers)
			if resp.StatusCode != tc.status {
				t.Fatalf("got %d %s", resp.StatusCode, body)
			}
			if tc.status == 401 && !strings.Contains(body, `"type":"authentication_error"`) {
				t.Fatalf("not an auth error: %s", body)
			}
		})
	}
}

func TestAuthTouchesLastUsedAsynchronously(t *testing.T) {
	e := newEnv(t)
	e.do(t, http.MethodGet, "/v1/models", "", map[string]string{"Authorization": "Bearer " + e.token})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		list, err := e.keys.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(list) > 0 && list[0].LastUsedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("last used never updated")
}

func TestAllowList(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, ok("{}"))
	e.route([]string{"a"})
	e.models["other"] = virtualmodels.Model{Name: "other", Kind: virtualmodels.KindChat, CreatedAt: time.Unix(42, 0)}
	_, only := e.newKey(t, "m")
	h := map[string]string{"Authorization": "Bearer " + only}

	resp, body := e.do(t, http.MethodPost, "/v1/chat/completions", `{"model":"other"}`, h)
	wantError(t, resp, body, http.StatusForbidden, "model_not_allowed")
	if resp, _ := e.do(t, http.MethodPost, "/v1/chat/completions", simple, h); resp.StatusCode != 200 {
		t.Fatalf("allowed model rejected: %d", resp.StatusCode)
	}

	_, body = e.do(t, http.MethodGet, "/v1/models", "", h)
	if strings.Contains(body, `"other"`) || !strings.Contains(body, `"id":"m"`) {
		t.Fatalf("filtered list: %s", body)
	}
	_, body = e.do(t, http.MethodGet, "/v1/models", "", map[string]string{"Authorization": "Bearer " + e.token})
	var list struct {
		Object string
		Data   []struct{ ID string }
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || list.Object != "list" || len(list.Data) != 2 {
		t.Fatalf("empty allow-list must allow all: %s", body)
	}
}
