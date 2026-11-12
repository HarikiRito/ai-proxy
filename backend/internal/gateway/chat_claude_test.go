package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

const claudeReply = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Hel"},{"type":"text","text":"lo"}],` +
	`"stop_reason":"end_turn","usage":{"input_tokens":10,"cache_read_input_tokens":4,"cache_creation_input_tokens":2,"output_tokens":7}}`

func claudeErr(status int, typ, msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"` + typ + `","message":"` + msg + `"}}`))
	}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("%v: %s", err, s)
	}
	return m
}

// sent posts a chat body to a lone Claude target and returns what that target received.
func (e *env) sent(t *testing.T, body string) (map[string]any, http.Header) {
	t.Helper()
	var got []byte
	var hdr http.Header
	e.claude(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		hdr = r.Header.Clone()
		ok(claudeReply)(w, r)
	})
	e.route([]string{"a"})
	if resp, out := e.chat(t, body); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, out)
	}
	return decode(t, string(got)), hdr
}

func TestChatToClaudeRequestTranslation(t *testing.T) {
	e := newEnv(t)
	got, hdr := e.sent(t, `{"model":"m","stream":true,"stream_options":{"include_usage":true},"temperature":0.5,"top_p":0.9,"stop":"END","user":"u1","seed":1,"presence_penalty":0.1,"logprobs":false,
		"messages":[{"role":"system","content":"be brief"},{"role":"developer","content":[{"type":"text","text":"be kind"}]},
		{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}},{"type":"image_url","image_url":{"url":"https://x.test/a.png"}}]},
		{"role":"assistant","content":"ok"},{"role":"user","content":"hi"}]}`)
	want := decode(t, `{"model":"real-a","stream":true,"temperature":0.5,"top_p":0.9,"stop_sequences":["END"],"metadata":{"user_id":"u1"},"max_tokens":8192,"cache_control":{"type":"ephemeral"},
		"system":"be brief\n\nbe kind","messages":[
		{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},{"type":"image","source":{"type":"url","url":"https://x.test/a.png"}}]},
		{"role":"assistant","content":[{"type":"text","text":"ok"}]},{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	if hdr.Get("X-Api-Key") != "real-k-a" || hdr.Get("Authorization") != "" || hdr.Get("Anthropic-Version") != "2023-06-01" {
		t.Fatalf("headers %v", hdr)
	}
}

func TestChatToClaudeTokensStopAndCacheControl(t *testing.T) {
	msgs := `"messages":[{"role":"user","content":"hi"}]`
	for name, tc := range map[string]struct {
		extra     string
		maxTokens float64
		stop      any
		cache     string
	}{
		"defaults":       {``, 8192, nil, `{"type":"ephemeral"}`},
		"max_tokens":     {`"max_tokens":50,`, 50, nil, `{"type":"ephemeral"}`},
		"max_completion": {`"max_tokens":50,"max_completion_tokens":70,`, 70, nil, `{"type":"ephemeral"}`},
		"stop list":      {`"stop":["a","b"],`, 8192, []any{"a", "b"}, `{"type":"ephemeral"}`},
		"client cache":   {`"cache_control":{"type":"ephemeral","ttl":"1h"},`, 8192, nil, `{"type":"ephemeral","ttl":"1h"}`},
	} {
		t.Run(name, func(t *testing.T) {
			got, _ := newEnv(t).sent(t, `{"model":"m",`+tc.extra+msgs+`}`)
			if got["max_tokens"] != tc.maxTokens || !reflect.DeepEqual(got["stop_sequences"], tc.stop) || !reflect.DeepEqual(got["cache_control"], decode(t, tc.cache)) {
				t.Fatalf("%v", got)
			}
		})
	}
}

func TestChatFromClaudeBufferedReply(t *testing.T) {
	e := newEnv(t)
	e.claude(t, "a", 1, ok(claudeReply))
	e.route([]string{"a"})
	before := time.Now().Add(clockSkew).Unix()
	resp, out := e.chat(t, simple)
	got := decode(t, out)
	created, _ := got["created"].(float64)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json" || int64(created) < before || int64(created) > before+10 {
		t.Fatalf("%d %v %s", resp.StatusCode, resp.Header, out)
	}
	got["created"] = 0.0
	want := decode(t, `{"id":"msg_1","object":"chat.completion","created":0,"model":"claude-x",
		"choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":16,"completion_tokens":7,"total_tokens":23,"prompt_tokens_details":{"cached_tokens":4}}}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s", out)
	}
}

func TestChatFromClaudeStopReasons(t *testing.T) {
	for stop, want := range map[string]string{
		"end_turn": "stop", "stop_sequence": "stop", "pause_turn": "stop", "max_tokens": "length",
		"tool_use": "tool_calls", "refusal": "content_filter", "something_new": "stop",
	} {
		e := newEnv(t)
		e.claude(t, "a", 1, ok(`{"id":"m","model":"c","content":[],"stop_reason":"`+stop+`","usage":{}}`))
		e.route([]string{"a"})
		_, out := e.chat(t, simple)
		if got := decode(t, out)["choices"].([]any)[0].(map[string]any)["finish_reason"]; got != want {
			t.Errorf("%s -> %v, want %s", stop, got, want)
		}
	}
}

func TestChatFromClaudeClientErrorsUseOpenAIEnvelope(t *testing.T) {
	for _, tc := range []struct {
		status int
		typ    string
		claude string
	}{
		{401, "authentication_error", "authentication_error"},
		{400, "invalid_request_error", "invalid_request_error"},
		{403, "permission_error", "permission_error"},
		{404, "not_found_error", "not_found_error"},
	} {
		e := newEnv(t)
		e.claude(t, "a", 1, claudeErr(tc.status, tc.claude, "bad thing"))
		e.route([]string{"a"})
		resp, out := e.chat(t, simple)
		var got chatErrorShape
		_ = json.Unmarshal([]byte(out), &got)
		if resp.StatusCode != tc.status || got.Error.Message != "bad thing" || got.Error.Type != tc.typ || got.Error.Code != tc.claude {
			t.Errorf("%d: %d %s", tc.status, resp.StatusCode, out)
		}
	}
}

type chatErrorShape struct {
	Error struct{ Message, Type, Code string }
}

// failingFirst routes to a failing Claude target "a" then a working "b" of the given kind.
func (e *env) failingFirst(t *testing.T, code int, nextOpenAI bool) *atomic.Int32 {
	t.Helper()
	h := status(code)
	if code == 429 {
		h = status(code, "Retry-After", "60")
	}
	down := e.upstreamAs(t, "a", providers.KindAnthropic, "/v1", 1, h)
	if nextOpenAI {
		e.upstream(t, "b", 1, ok(`{"from":"openai"}`))
	} else {
		e.claude(t, "b", 1, ok(claudeReply))
	}
	e.route([]string{"a", "b"})
	return down
}

func TestChatClaudeFailoverKeepsCooldownAndTargetKinds(t *testing.T) {
	for _, nextOpenAI := range []bool{false, true} {
		for code, wantHits := range map[int]int32{429: 1, 529: 2, 500: 2} {
			e := newEnv(t)
			down := e.failingFirst(t, code, nextOpenAI)
			var out string
			for range 2 {
				_, out = e.chat(t, simple)
			}
			if nextOpenAI != (out == `{"from":"openai"}`) || down.Load() != wantHits {
				t.Errorf("openai=%v %d: out %s, failing target hit %d", nextOpenAI, code, out, down.Load())
			}
		}
	}
}

func TestChatUntranslatableRequestsOnlyReachOpenAITargets(t *testing.T) {
	const msg = `"messages":[{"role":"user","content":"hi"}]`
	for name, body := range map[string]string{
		"tools":          `{"model":"m",` + msg + `,"tools":[{"type":"function","function":{"name":"f"}}]}`,
		"tool_choice":    `{"model":"m",` + msg + `,"tool_choice":"auto"}`,
		"functions":      `{"model":"m",` + msg + `,"functions":[{"name":"f"}]}`,
		"parallel":       `{"model":"m",` + msg + `,"parallel_tool_calls":true}`,
		"tool message":   `{"model":"m","messages":[{"role":"tool","tool_call_id":"1","content":"x"}]}`,
		"tool_calls":     `{"model":"m","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"1"}]}]}`,
		"n=2":            `{"model":"m",` + msg + `,"n":2}`,
		"json_object":    `{"model":"m",` + msg + `,"response_format":{"type":"json_object"}}`,
		"audio":          `{"model":"m",` + msg + `,"audio":{"voice":"x"}}`,
		"modalities":     `{"model":"m",` + msg + `,"modalities":["text","audio"]}`,
		"audio part":     `{"model":"m","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{}}]}]}`,
		"file image":     `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"file:///x"}}]}]}`,
		"no messages":    `{"model":"m"}`,
		"unknown role":   `{"model":"m","messages":[{"role":"critic","content":"x"}]}`,
		"malformed stop": `{"model":"m",` + msg + `,"stop":5}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			claude := e.upstreamAs(t, "a", providers.KindAnthropic, "/v1", 1, ok(claudeReply))
			e.route([]string{"a"})
			resp, out := e.chat(t, body)
			wantError(t, resp, out, http.StatusServiceUnavailable, "no_available_provider")

			openai := e.upstream(t, "o", 1, ok(`{"from":"openai"}`))
			e.route([]string{"a"}, []string{"o"})
			if _, out := e.chat(t, body); out != `{"from":"openai"}` || claude.Load() != 0 || openai.Load() != 1 {
				t.Fatalf("%s, claude hit %d openai %d", out, claude.Load(), openai.Load())
			}
		})
	}
}

func TestChatMixedChainServesBothKinds(t *testing.T) {
	e := newEnv(t)
	openai := e.upstream(t, "o", 1, ok(`{"from":"openai"}`))
	claude := e.upstreamAs(t, "a", providers.KindAnthropic, "/v1", 1, ok(claudeReply))
	e.route([]string{"o", "a"})
	m := e.models["m"]
	m.Strategy = virtualmodels.StrategyRoundRobin
	e.models["m"] = m
	for range 6 {
		e.chat(t, simple)
	}
	if openai.Load() == 0 || claude.Load() == 0 {
		t.Fatalf("openai %d claude %d", openai.Load(), claude.Load())
	}
}

func (e *env) openChat(t *testing.T, ctx context.Context, body string) (reply, *bufio.Reader) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.srv.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return reply{resp.StatusCode, resp.Header}, bufio.NewReader(resp.Body)
}

func claudeEvent(typ, data string) string { return "event: " + typ + "\ndata: " + data + "\n\n" }

const (
	evStart = `{"type":"message_start","message":{"id":"msg_1","model":"claude-x","usage":{"input_tokens":10,"cache_read_input_tokens":4,"cache_creation_input_tokens":2,"output_tokens":1}}}`
	evStop  = `{"type":"message_stop"}`

	chatChunkHead = `data: {"id":"msg_1","object":"chat.completion.chunk","created":0,"model":"claude-x","choices":[{"index":0,"delta":`
	roleChunk     = chatChunkHead + `{"role":"assistant","content":""},"finish_reason":null}]}` + "\n\n"
)

var claudeStream = claudeEvent("message_start", evStart) +
	claudeEvent("ping", `{"type":"ping"}`) +
	claudeEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`) +
	claudeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`) +
	claudeEvent("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hel"}}`) +
	claudeEvent("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"lo"}}`) +
	claudeEvent("content_block_stop", `{"type":"content_block_stop","index":1}`) +
	claudeEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":7}}`) +
	claudeEvent("message_stop", evStop)

const (
	wantChunks = roleChunk +
		chatChunkHead + `{"content":"Hel"},"finish_reason":null}]}` + "\n\n" +
		chatChunkHead + `{"content":"lo"},"finish_reason":null}]}` + "\n\n" +
		chatChunkHead + `{},"finish_reason":"length"}]}` + "\n\n"
	wantUsageChunk = `data: {"id":"msg_1","object":"chat.completion.chunk","created":0,"model":"claude-x","choices":[],` +
		`"usage":{"prompt_tokens":16,"completion_tokens":7,"total_tokens":23,"prompt_tokens_details":{"cached_tokens":4}}}` + "\n\n"
)

var createdRe = regexp.MustCompile(`"created":\d+`)

func normalized(s string) string { return createdRe.ReplaceAllString(s, `"created":0`) }

func (e *env) streamed(t *testing.T, body string, h http.HandlerFunc) string {
	t.Helper()
	e.claude(t, "a", 1, h)
	e.route([]string{"a"})
	resp, r := e.openChat(t, context.Background(), body)
	all, _ := io.ReadAll(r)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
	return normalized(string(all))
}

func TestChatStreamTranslatesEvents(t *testing.T) {
	const body = `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]`
	for name, tc := range map[string]struct{ body, want string }{
		"no usage":      {body + `}`, wantChunks + "data: [DONE]\n\n"},
		"include usage": {body + `,"stream_options":{"include_usage":true}}`, wantChunks + wantUsageChunk + "data: [DONE]\n\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := newEnv(t).streamed(t, tc.body, sse(claudeStream)); got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestChatStreamHandlesArbitraryChunkBoundaries(t *testing.T) {
	got := newEnv(t).streamed(t, streamReq, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < len(claudeStream); i += 7 {
			flushed(w, claudeStream[i:min(i+7, len(claudeStream))])
		}
	})
	if got != wantChunks+"data: [DONE]\n\n" {
		t.Fatalf("got %q", got)
	}
}

func TestChatStreamFlushesFirstChunkBeforeUpstreamFinishes(t *testing.T) {
	e := newEnv(t)
	finish := make(chan struct{})
	e.claude(t, "a", 1, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, claudeEvent("message_start", evStart))
		<-finish
		flushed(w, claudeEvent("message_stop", evStop))
	})
	e.route([]string{"a"})
	_, r := e.openChat(t, context.Background(), streamReq)
	first, err := r.ReadString('\n')
	if err != nil || normalized(first) != strings.SplitAfter(roleChunk, "\n")[0] {
		t.Fatalf("first %q %v", first, err)
	}
	close(finish)
	if rest, _ := io.ReadAll(r); string(rest) != "\ndata: [DONE]\n\n" {
		t.Fatalf("rest %q", rest)
	}
}

func TestChatStreamUpstreamErrorEvent(t *testing.T) {
	got := newEnv(t).streamed(t, streamReq, sse(
		claudeEvent("message_start", evStart),
		claudeEvent("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`),
	))
	want := roleChunk + `data: {"error":{"message":"Overloaded","type":"upstream_error","code":"overloaded_error"}}` + "\n\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestChatStreamTruncatedUpstreamSendsErrorEventAndFreesLease(t *testing.T) {
	e := newEnv(t)
	got := e.streamed(t, streamReq, onceThen(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, claudeEvent("message_start", evStart))
		c, _, _ := w.(http.Hijacker).Hijack()
		_ = c.Close()
	}, ok(claudeReply)))
	want := roleChunk + "\n\n" + `data: {"error":{"code":"stream_interrupted","message":"upstream stream interrupted","type":"upstream_error"}}` + "\n\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if _, out := e.chat(t, simple); !strings.Contains(out, `"Hello"`) {
		t.Fatalf("lease not freed: %s", out)
	}
}

func TestChatStreamCleanEOFBeforeStopIsAnError(t *testing.T) {
	got := newEnv(t).streamed(t, streamReq, sse(claudeEvent("message_start", evStart)))
	if !strings.Contains(got, `"code":"stream_interrupted"`) || strings.Contains(got, "[DONE]") {
		t.Fatalf("got %q", got)
	}
}

func TestChatStreamClientDisconnectCancelsUpstreamAndFreesLease(t *testing.T) {
	e := newEnv(t)
	cancelled := make(chan struct{})
	e.claude(t, "a", 1, onceThen(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, claudeEvent("message_start", evStart))
		<-r.Context().Done()
		close(cancelled)
	}, ok(claudeReply)))
	e.route([]string{"a"})
	ctx, cancel := context.WithCancel(context.Background())
	_, r := e.openChat(t, ctx, streamReq)
	if _, err := r.ReadString('\n'); err != nil {
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
		if _, out := e.chat(t, simple); strings.Contains(out, `"Hello"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("lease not freed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
