package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
)

const (
	sidecarCred = "real-k-a" // what the fake provider list reveals for key k-a
	urlImage    = `{"type":"image","source":{"type":"url","url":"https://x.test/a.png"}}`
)

type seen struct {
	header http.Header
	path   string
	body   string
}

// sidecar registers a subscription provider whose one key uses mode; expires is optional.
func (e *env) sidecar(t *testing.T, id string, mode providers.AuthMode, expires *time.Time, h http.HandlerFunc) *atomic.Int32 {
	t.Helper()
	hits := e.upstreamAs(t, id, providers.KindClaudeSubscription, "", 2, h)
	p := e.provs[id]
	p.Keys[0].AuthMode, p.Keys[0].TokenExpiresAt = mode, expires
	e.provs[id] = p
	return hits
}

func recording(into *seen, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*into = seen{r.Header.Clone(), r.URL.Path, string(body)}
		h(w, r)
	}
}

func (e *env) engineNow() time.Time {
	return time.Now().Add(clockSkew + time.Duration(e.advance.Load()))
}

func (e *env) flagOf(t *testing.T, keyID string) string {
	t.Helper()
	snap, err := e.engine.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range snap {
		if k.KeyID == keyID {
			return k.Flag
		}
	}
	t.Fatalf("key %s not in snapshot", keyID)
	return ""
}

func authError(status int, typ string, header ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i+1 < len(header); i += 2 {
			w.Header().Set(header[i], header[i+1])
		}
		claudeErr(status, typ, "nope")(w, r)
	}
}

func (e *env) wantSidecarCall(t *testing.T, got seen, mode providers.AuthMode, out string) {
	t.Helper()
	if got.path != "/v1/messages" || got.header.Get("X-Claude-Auth-Mode") != string(mode) || got.header.Get("X-Claude-Credential") != sidecarCred {
		t.Fatalf("%s %v", got.path, got.header)
	}
	if got.header.Get("X-Api-Key") != "" || got.header.Get("Authorization") != "" {
		t.Fatalf("proxy or provider credentials leaked to the sidecar: %v", got.header)
	}
	if want, have := [4]int{16, 7, 4, 2}, tokens(e.one(t)); have != want {
		t.Fatalf("tokens %v, want %v", have, want)
	}
	if strings.Contains(out, sidecarCred) || strings.Contains(e.logs.String(), sidecarCred) {
		t.Fatal("credential leaked to the client or the logs")
	}
}

func TestSubscriptionTargetsGetSidecarCredentialsAndRecordCacheUsage(t *testing.T) {
	calls := map[string]func(e *env) (reply, string){
		"chat":            func(e *env) (reply, string) { return e.chat(t, simple) },
		"chat stream":     func(e *env) (reply, string) { return e.chat(t, streamReq) },
		"messages":        func(e *env) (reply, string) { return e.messages(t, claudeReq, "X-Claude-Credential", "evil") },
		"messages stream": func(e *env) (reply, string) { return e.messages(t, claudeStreamReq, "X-Claude-Auth-Mode", "evil") },
	}
	for _, mode := range []providers.AuthMode{providers.AuthSetupToken, providers.AuthLoginDir} {
		for name, call := range calls {
			t.Run(string(mode)+" "+name, func(t *testing.T) {
				e := newEnv(t)
				var got seen
				h := ok(claudeReply)
				if strings.Contains(name, "stream") {
					h = claudeSSE(claudeStart, claudeDelta)
				}
				e.sidecar(t, "a", mode, nil, recording(&got, h))
				e.route([]string{"a"})
				resp, out := call(e)
				if resp.StatusCode != 200 {
					t.Fatalf("%d %s", resp.StatusCode, out)
				}
				e.wantSidecarCall(t, got, mode, out)
			})
		}
	}
}

func TestSubscriptionConversationPinsOnCacheRead(t *testing.T) {
	e := newEnv(t)
	a := e.sidecar(t, "a", providers.AuthLoginDir, nil, ok(claudeReply))
	b := e.sidecar(t, "b", providers.AuthLoginDir, nil, ok(claudeReply))
	e.affine("a", "b")
	for range 10 {
		if resp, body := e.messages(t, claudeReq); resp.StatusCode != 200 {
			t.Fatalf("%d %s", resp.StatusCode, body)
		}
	}
	if a.Load()+b.Load() != 10 || (a.Load() != 0 && b.Load() != 0) {
		t.Fatalf("a=%d b=%d", a.Load(), b.Load())
	}
}

func TestSubscriptionRateLimitCoolsKeyAndFailsOver(t *testing.T) {
	e := newEnv(t)
	a := e.sidecar(t, "a", providers.AuthLoginDir, nil, authError(429, "rate_limit_error", "Retry-After", "120"))
	e.claude(t, "b", 1, ok(`{"served":"b"}`))
	e.route([]string{"a"}, []string{"b"})
	for range 2 {
		if _, out := e.messages(t, claudeReq); out != `{"served":"b"}` {
			t.Fatal(out)
		}
	}
	if a.Load() != 1 {
		t.Fatalf("cooled key was called %d times", a.Load())
	}
}

// authFlow is a failing subscription target "a" with a healthy Anthropic fallback "b".
type authFlow struct {
	t       *testing.T
	e       *env
	a       *atomic.Int32
	healthy atomic.Bool
}

func newAuthFlow(t *testing.T, status int) *authFlow {
	f := &authFlow{t: t, e: newEnv(t)}
	f.a = f.e.sidecar(t, "a", providers.AuthLoginDir, nil, func(w http.ResponseWriter, r *http.Request) {
		if f.healthy.Load() {
			ok(`{"served":"a"}`)(w, r)
			return
		}
		authError(status, "authentication_error")(w, r)
	})
	f.e.claude(t, "b", 1, ok(`{"served":"b"}`))
	f.e.route([]string{"a"}, []string{"b"})
	return f
}

func (f *authFlow) serve(wantBody string) {
	f.t.Helper()
	if _, out := f.e.messages(f.t, claudeReq); out != wantBody {
		f.t.Fatalf("served %s, want %s", out, wantBody)
	}
}

func (f *authFlow) want(hits int32, flag string) {
	f.t.Helper()
	if got := f.e.flagOf(f.t, "k-a"); f.a.Load() != hits || got != flag {
		f.t.Fatalf("hits %d flag %q, want %d %q", f.a.Load(), got, hits, flag)
	}
}

func (f *authFlow) pass() { f.e.advance.Add(int64(11 * time.Minute)) }

func TestSubscriptionAuthFailureFlagsKeyUntilProbeSucceeds(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newAuthFlow(t, status)
			f.serve(`{"served":"b"}`)
			f.want(1, "needs_login")
			f.serve(`{"served":"b"}`)
			f.want(1, "needs_login") // skipped while flagged

			f.pass()
			f.serve(`{"served":"b"}`) // probe fails and flags again
			f.serve(`{"served":"b"}`)
			f.want(2, "needs_login")

			f.healthy.Store(true)
			f.pass()
			f.serve(`{"served":"a"}`)
			f.want(3, "")
			f.serve(`{"served":"a"}`)
		})
	}
}

func TestAuthFailureOfAnthropicTargetStillPassesThrough(t *testing.T) {
	e := newEnv(t)
	e.claude(t, "a", 1, authError(401, "authentication_error"))
	e.claude(t, "b", 1, ok(`{}`))
	e.route([]string{"a"}, []string{"b"})
	if resp, _ := e.messages(t, claudeReq); resp.StatusCode != 401 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestSubscriptionExpiredTokenIsSkipped(t *testing.T) {
	e := newEnv(t)
	expired, fresh := e.engineNow().Add(-time.Hour), e.engineNow().Add(30*24*time.Hour)
	a := e.sidecar(t, "a", providers.AuthSetupToken, &expired, ok(`{"served":"a"}`))
	b := e.sidecar(t, "b", providers.AuthSetupToken, &fresh, ok(`{"served":"b"}`))
	e.route([]string{"a"}, []string{"b"})
	if _, out := e.messages(t, claudeReq); out != `{"served":"b"}` || a.Load() != 0 || b.Load() != 1 {
		t.Fatalf("%s a=%d b=%d", out, a.Load(), b.Load())
	}
	if e.flagOf(t, "k-a") != "expired" || e.flagOf(t, "k-b") != "" {
		t.Fatalf("flags %q %q", e.flagOf(t, "k-a"), e.flagOf(t, "k-b"))
	}
}

func TestIgnoredParamsHeaderOnlyWhenSubscriptionDropsThem(t *testing.T) {
	const sampled = `{"model":"m","temperature":0.2,"top_p":0.9,"max_tokens":50,"stop":"x","top_k":null,"messages":[{"role":"user","content":"hi"}]}`
	const nativeSampled = `{"model":"m","top_k":3,"temperature":0.2,"max_tokens":9,"messages":[{"role":"user","content":"hi"}]}`
	const want = "max_tokens,stop,temperature,top_p"
	cases := map[string]struct {
		call   func(e *env) (reply, string)
		stream bool
		want   string
	}{
		"chat": {func(e *env) (reply, string) { return e.chat(t, sampled) }, false, want},
		"chat stream": {func(e *env) (reply, string) {
			return e.chat(t, strings.Replace(sampled, `"model":"m"`, `"model":"m","stream":true`, 1))
		}, true, want},
		"messages": {func(e *env) (reply, string) { return e.messages(t, nativeSampled) }, false, "max_tokens,temperature,top_k"},
		"messages stream": {func(e *env) (reply, string) {
			return e.messages(t, strings.Replace(nativeSampled, `"model":"m"`, `"model":"m","stream":true`, 1))
		}, true, "max_tokens,temperature,top_k"},
		"nothing sent":        {func(e *env) (reply, string) { return e.chat(t, simple) }, false, ""},
		"nothing sent stream": {func(e *env) (reply, string) { return e.chat(t, streamReq) }, true, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			h := ok(claudeReply)
			if tc.stream {
				h = claudeSSE(claudeStart, claudeDelta)
			}
			e.sidecar(t, "a", providers.AuthLoginDir, nil, h)
			e.route([]string{"a"})
			resp, out := tc.call(e)
			if resp.StatusCode != 200 || resp.Header.Get("X-Proxy-Ignored-Params") != tc.want {
				t.Fatalf("%d header %q: %s", resp.StatusCode, resp.Header.Get("X-Proxy-Ignored-Params"), out)
			}
		})
	}
}

func TestIgnoredParamsHeaderAbsentOnOtherTargets(t *testing.T) {
	e := newEnv(t)
	e.claude(t, "a", 1, ok(claudeReply))
	e.route([]string{"a"})
	resp, _ := e.messages(t, `{"model":"m","temperature":0.2,"max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != 200 || resp.Header.Get("X-Proxy-Ignored-Params") != "" {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
}

func TestSubscriptionSkippedForRequestsTheSidecarCannotRun(t *testing.T) {
	native := map[string]string{
		"tools":       `{"model":"m","max_tokens":5,"tools":[{"name":"t","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`,
		"tool_choice": `{"model":"m","max_tokens":5,"tool_choice":{"type":"auto"},"messages":[{"role":"user","content":"hi"}]}`,
		"url image":   `{"model":"m","max_tokens":5,"messages":[{"role":"user","content":[` + urlImage + `]}]}`,
	}
	for name, body := range native {
		t.Run("messages "+name, func(t *testing.T) {
			e := newEnv(t)
			sub := e.sidecar(t, "a", providers.AuthLoginDir, nil, ok(`{"served":"a"}`))
			e.claude(t, "b", 1, ok(`{"served":"b"}`))
			e.route([]string{"a", "b"})
			if _, out := e.messages(t, body); out != `{"served":"b"}` || sub.Load() != 0 {
				t.Fatalf("%s sidecar hits %d", out, sub.Load())
			}
			e.route([]string{"a"})
			resp, out := e.messages(t, body)
			wantAnthropic(t, resp, out, http.StatusServiceUnavailable, "api_error")
		})
	}
	t.Run("chat url image", func(t *testing.T) {
		e := newEnv(t)
		sub := e.sidecar(t, "a", providers.AuthLoginDir, nil, ok(`{"served":"a"}`))
		e.claude(t, "b", 1, ok(claudeReply))
		e.route([]string{"a", "b"})
		body := `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://x.test/a.png"}}]}]}`
		if resp, out := e.chat(t, body); resp.StatusCode != 200 || sub.Load() != 0 {
			t.Fatalf("%d %s sidecar hits %d", resp.StatusCode, out, sub.Load())
		}
		data := `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`
		if e.chat(t, data); sub.Load() != 1 {
			t.Fatalf("base64 image should reach the sidecar, hits %d", sub.Load())
		}
	})
}

func TestTranscriptionNeverRoutesToSubscription(t *testing.T) {
	e := newEnv(t)
	sub := e.sidecar(t, "a", providers.AuthLoginDir, nil, ok(`{"text":"a"}`))
	e.audio(t, []string{"a"})
	body, ctype := sampleForm(t, "x")
	if resp, _ := e.transcribe(t, body, ctype); resp.StatusCode != http.StatusServiceUnavailable || sub.Load() != 0 {
		t.Fatalf("status %d hits %d", resp.StatusCode, sub.Load())
	}
}
