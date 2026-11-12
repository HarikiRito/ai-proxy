package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

const (
	claudeReq       = `{"max_tokens":10,"messages":[{"content":"hi","role":"user"}],"model":"m"}`
	claudeStreamReq = `{"max_tokens":10,"messages":[{"content":"hi","role":"user"}],"model":"m","stream":true}`
)

func (e *env) claude(t *testing.T, id string, limit int, h http.HandlerFunc) {
	t.Helper()
	e.upstreamAs(t, id, providers.KindAnthropic, "/v1", limit, h)
}

func (e *env) messages(t *testing.T, body string, headers ...string) (reply, string) {
	t.Helper()
	h := map[string]string{"x-api-key": e.token}
	for i := 0; i+1 < len(headers); i += 2 {
		h[headers[i]] = headers[i+1]
	}
	return e.do(t, http.MethodPost, "/v1/messages", body, h)
}

func wantAnthropic(t *testing.T, resp reply, body string, status int, typ string) {
	t.Helper()
	var out struct {
		Type  string
		Error struct{ Type, Message string }
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || resp.StatusCode != status || out.Type != "error" || out.Error.Type != typ || out.Error.Message == "" {
		t.Fatalf("want %d %s, got %d %s", status, typ, resp.StatusCode, body)
	}
}

func TestMessagesPassThroughKeepsBodyAndSwapsCredentials(t *testing.T) {
	const client = `{"anthropic_beta_field":{"x": 1,  "y":[1, 2]},"max_tokens":10,"messages":[{"content":[{"text":"a <b>","type":"text"}],"role":"user"}],"model":"m","thinking":{"budget_tokens":5,"type":"enabled"}}`
	e := newEnv(t)
	var gotBody []byte
	var got http.Header
	var gotPath string
	e.claude(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		got, gotPath = r.Header.Clone(), r.URL.Path
		ok(`{"type":"message"}`)(w, r)
	})
	e.route([]string{"a"})
	_, out := e.messages(t, client, "Anthropic-Version", "2024-01-01", "Anthropic-Beta", "x-1,x-2", "Authorization", "Bearer "+e.token)
	if want := strings.Replace(client, `"model":"m"`, `"model":"real-a"`, 1); string(gotBody) != want || out != `{"type":"message"}` {
		t.Fatalf("body %s, reply %s", gotBody, out)
	}
	if gotPath != "/v1/messages" || got.Get("X-Api-Key") != "real-k-a" || got.Get("Authorization") != "" ||
		got.Get("Anthropic-Version") != "2024-01-01" || got.Get("Anthropic-Beta") != "x-1,x-2" || got.Get("Content-Type") != "application/json" {
		t.Fatalf("path %s headers %v", gotPath, got)
	}
	for k, vs := range got {
		if strings.Contains(strings.Join(vs, ","), e.token) {
			t.Errorf("proxy key leaked in %s", k)
		}
	}
}

func TestMessagesDefaultsAnthropicVersion(t *testing.T) {
	e := newEnv(t)
	var version string
	e.claude(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		version = r.Header.Get("Anthropic-Version")
		ok("{}")(w, r)
	})
	e.route([]string{"a"})
	e.messages(t, claudeReq)
	if version != "2023-06-01" {
		t.Fatalf("version %q", version)
	}
}

func TestMessagesBaseURLWithAndWithoutV1(t *testing.T) {
	for _, suffix := range []string{"", "/v1", "/v1/"} {
		e := newEnv(t)
		var path string
		e.upstreamAs(t, "a", providers.KindAnthropic, suffix, 1, func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			ok("{}")(w, r)
		})
		e.route([]string{"a"})
		e.messages(t, claudeReq)
		if path != "/v1/messages" {
			t.Errorf("base suffix %q hit %q", suffix, path)
		}
	}
}

func (e *env) openMessages(t *testing.T, ctx context.Context) io.Reader {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.srv.URL+"/v1/messages", strings.NewReader(claudeStreamReq))
	req.Header.Set("x-api-key", e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp.Body
}

func TestMessagesStreamFlushesIncrementallyAndPassesBytesThrough(t *testing.T) {
	e := newEnv(t)
	finish := make(chan struct{})
	e.claude(t, "a", 1, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "event: message_start\ndata: {\"n\":1}\n\n")
		<-finish
		flushed(w, "event: message_stop\ndata: {\"n\":2}\n\n")
	})
	e.route([]string{"a"})
	br := bufio.NewReader(e.openMessages(t, context.Background()))
	first, err := br.ReadString('\n')
	if err != nil || first != "event: message_start\n" {
		t.Fatalf("first chunk %q %v", first, err)
	}
	close(finish)
	rest, _ := io.ReadAll(br)
	if first+string(rest) != "event: message_start\ndata: {\"n\":1}\n\nevent: message_stop\ndata: {\"n\":2}\n\n" {
		t.Fatalf("bytes changed: %q", first+string(rest))
	}
}

func TestMessagesStreamBreakSendsAnthropicErrorEventAndFreesLease(t *testing.T) {
	e := newEnv(t)
	e.claude(t, "a", 1, onceThen(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "event: ping\ndata: {}\n\n")
		c, _, _ := w.(http.Hijacker).Hijack()
		_ = c.Close()
	}, ok(`{"ok":true}`)))
	e.route([]string{"a"})
	_, body := e.messages(t, claudeStreamReq)
	want := "event: ping\ndata: {}\n\n\n\nevent: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"upstream stream interrupted\",\"type\":\"api_error\"}}\n\n"
	if body != want {
		t.Fatalf("got %q", body)
	}
	if _, body := e.messages(t, claudeReq); body != `{"ok":true}` {
		t.Fatalf("lease not freed: %s", body)
	}
}

func TestMessagesClientDisconnectCancelsUpstreamAndFreesLease(t *testing.T) {
	e := newEnv(t)
	cancelled := make(chan struct{})
	e.claude(t, "a", 1, onceThen(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "event: ping\n")
		<-r.Context().Done()
		close(cancelled)
	}, ok(`{"ok":true}`)))
	e.route([]string{"a"})
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := bufio.NewReader(e.openMessages(t, ctx)).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, body := e.messages(t, claudeReq); body == `{"ok":true}` {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("lease not freed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMessagesOnlyHitsAnthropicTargets(t *testing.T) {
	e := newEnv(t)
	openai := e.upstream(t, "o", 1, ok(`{"from":"openai"}`))
	e.claude(t, "a", 1, ok(`{"from":"anthropic"}`))
	e.route([]string{"o"}, []string{"a"})
	if _, out := e.messages(t, claudeReq); out != `{"from":"anthropic"}` || openai.Load() != 0 {
		t.Fatalf("%s, openai hit %d", out, openai.Load())
	}

	e.route([]string{"o"})
	resp, out := e.messages(t, claudeReq)
	wantAnthropic(t, resp, out, http.StatusServiceUnavailable, "api_error")
	if openai.Load() != 0 {
		t.Fatal("openai target contacted")
	}
}

func TestChatSkipsAnthropicTargets(t *testing.T) {
	e := newEnv(t)
	hits := e.upstreamAs(t, "a", providers.KindAnthropic, "/v1", 1, ok("{}"))
	e.route([]string{"a"})
	resp, out := e.chat(t, simple)
	wantError(t, resp, out, http.StatusServiceUnavailable, "no_available_provider")
	if hits.Load() != 0 {
		t.Fatal("anthropic target contacted by chat")
	}
}

func TestMessagesErrorsUseAnthropicFormat(t *testing.T) {
	e := newEnv(t)
	e.settings.s.MaxBodyBytes = 200
	e.claude(t, "a", 1, ok("{}"))
	e.route([]string{"a"})
	e.models["audio"] = virtualmodels.Model{Name: "audio", Kind: virtualmodels.KindTranscription}
	revoked, revokedToken := e.newKey(t)
	if _, err := e.keys.Revoke(context.Background(), revoked.ID); err != nil {
		t.Fatal(err)
	}
	_, narrow := e.newKey(t, "other")
	for name, tc := range map[string]struct {
		body    string
		headers []string
		status  int
		typ     string
	}{
		"missing key":   {claudeReq, []string{"x-api-key", ""}, 401, "authentication_error"},
		"invalid key":   {claudeReq, []string{"x-api-key", "sk-nope"}, 401, "authentication_error"},
		"revoked key":   {claudeReq, []string{"x-api-key", revokedToken}, 401, "authentication_error"},
		"not allowed":   {claudeReq, []string{"x-api-key", narrow}, 403, "permission_error"},
		"unknown model": {`{"model":"nope"}`, nil, 404, "not_found_error"},
		"too large":     {`{"model":"m","pad":"` + strings.Repeat("x", 300) + `"}`, nil, 413, "request_too_large"},
		"wrong kind":    {`{"model":"audio"}`, nil, 400, "invalid_request_error"},
		"not json":      {`nope`, nil, 400, "invalid_request_error"},
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := e.messages(t, tc.body, tc.headers...)
			wantAnthropic(t, resp, body, tc.status, tc.typ)
		})
	}
}

func TestOpenAIRoutesKeepOpenAIErrors(t *testing.T) {
	e := newEnv(t)
	_, narrow := e.newKey(t, "other")
	resp, body := e.do(t, http.MethodPost, "/v1/chat/completions", simple, nil)
	wantError(t, resp, body, http.StatusUnauthorized, "missing_api_key")
	resp, body = e.do(t, http.MethodPost, "/v1/chat/completions", simple, map[string]string{"Authorization": "Bearer " + narrow})
	wantError(t, resp, body, http.StatusForbidden, "model_not_allowed")
}

func TestMessagesFullChainAnswers429WithRetryAfter(t *testing.T) {
	e := newEnv(t)
	b := newBlocker()
	e.claude(t, "a", 1, b.handlerFunc)
	e.route([]string{"a"})
	first := make(chan struct{})
	go func() {
		defer close(first)
		e.messages(t, claudeReq)
	}()
	<-b.arrived
	resp, body := e.messages(t, claudeReq)
	wantAnthropic(t, resp, body, http.StatusTooManyRequests, "rate_limit_error")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	close(b.release)
	<-first
}

func TestMessagesUpstreamFailureHandling(t *testing.T) {
	e := newEnv(t)
	limited := e.upstreamAs(t, "a", providers.KindAnthropic, "/v1", 1, status(429, "Retry-After", "60"))
	e.claude(t, "b", 1, ok(`{"from":"b"}`))
	e.route([]string{"a", "b"})
	for range 2 {
		if _, out := e.messages(t, claudeReq); out != `{"from":"b"}` {
			t.Fatalf("got %s", out)
		}
	}
	if limited.Load() != 1 {
		t.Fatalf("cooled key hit %d times", limited.Load())
	}

	for _, code := range []int{529, 500} {
		e := newEnv(t)
		down := e.upstreamAs(t, "a", providers.KindAnthropic, "/v1", 1, status(code))
		e.claude(t, "b", 1, ok(`{"from":"b"}`))
		e.route([]string{"a", "b"})
		for range 2 {
			e.messages(t, claudeReq)
		}
		if _, out := e.messages(t, claudeReq); out != `{"from":"b"}` || down.Load() != 3 {
			t.Fatalf("%d: %s, failing target hit %d (no cooldown expected)", code, out, down.Load())
		}
	}

	e = newEnv(t)
	e.claude(t, "a", 1, status(401))
	e.route([]string{"a"})
	resp, out := e.messages(t, claudeReq)
	if resp.StatusCode != 401 || out != `{"error":"upstream says no"}` {
		t.Fatalf("passthrough: %d %s", resp.StatusCode, out)
	}
}
