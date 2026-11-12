package gateway

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/usage"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

type fakeRecorder struct {
	mu   sync.Mutex
	recs []usage.Record
}

func (f *fakeRecorder) Record(r usage.Record) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recs = append(f.recs, r)
}

func (f *fakeRecorder) all() []usage.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]usage.Record(nil), f.recs...)
}

// wait returns the records once n arrived; streams record after the response ends.
func (f *fakeRecorder) wait(t *testing.T, n int) []usage.Record {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if got := f.all(); len(got) >= n {
			return got
		}
	}
	t.Fatalf("want %d records, got %d", n, len(f.all()))
	return nil
}

func (e *env) one(t *testing.T) usage.Record {
	t.Helper()
	recs := e.rec.wait(t, 1)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	return recs[0]
}

func tokens(r usage.Record) [4]int {
	v := func(p *int) int {
		if p == nil {
			return -1
		}
		return *p
	}
	return [4]int{v(r.InputTokens), v(r.OutputTokens), v(r.CacheReadTokens), v(r.CacheCreationTokens)}
}

func claudeSSE(usageStart, usageDelta string) http.HandlerFunc {
	return sse(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"model\":\"c\",\"usage\":"+usageStart+"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":"+usageDelta+"}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	)
}

const (
	claudeStart = `{"input_tokens":10,"cache_read_input_tokens":4,"cache_creation_input_tokens":2,"output_tokens":1}`
	claudeDelta = `{"output_tokens":7}`
)

func TestRecordsTokensPerEndpointAndProviderKind(t *testing.T) {
	openAIReply := `{"usage":{"prompt_tokens":12,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":8}}}`
	for name, tc := range map[string]struct {
		kind   providers.Kind
		suffix string
		h      http.HandlerFunc
		call   func(e *env) (reply, string)
		want   [4]int
	}{
		"openai buffered": {providers.KindOpenAICompatible, "/v1", ok(openAIReply), func(e *env) (reply, string) { return e.chat(t, simple) }, [4]int{12, 5, 8, -1}},
		"openai stream with usage chunk": {
			providers.KindOpenAICompatible, "/v1", sse("data: {\"choices\":[{}]}\n\n", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"prompt_tokens_details\":{\"cached_tokens\":1}}}\n\n", "data: [DONE]\n\n"),
			func(e *env) (reply, string) { return e.chat(t, streamReq) },
			[4]int{3, 2, 1, -1},
		},
		"openai stream without usage chunk": {
			providers.KindOpenAICompatible, "/v1", sse("data: {\"choices\":[{}],\"usage\":null}\n\n", "data: [DONE]\n\n"),
			func(e *env) (reply, string) { return e.chat(t, streamReq) },
			[4]int{-1, -1, -1, -1},
		},
		"claude buffered via chat": {providers.KindAnthropic, "/v1", ok(claudeReply), func(e *env) (reply, string) { return e.chat(t, simple) }, [4]int{16, 7, 4, 2}},
		"claude stream via chat":   {providers.KindAnthropic, "/v1", claudeSSE(claudeStart, claudeDelta), func(e *env) (reply, string) { return e.chat(t, streamReq) }, [4]int{16, 7, 4, 2}},
		"native buffered":          {providers.KindAnthropic, "/v1", ok(claudeReply), func(e *env) (reply, string) { return e.messages(t, claudeReq) }, [4]int{16, 7, 4, 2}},
		"native stream":            {providers.KindAnthropic, "/v1", claudeSSE(claudeStart, claudeDelta), func(e *env) (reply, string) { return e.messages(t, claudeStreamReq) }, [4]int{16, 7, 4, 2}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.upstreamAs(t, "a", tc.kind, tc.suffix, 1, tc.h)
			e.route([]string{"a"})
			if resp, body := tc.call(e); resp.StatusCode != 200 {
				t.Fatalf("%d %s", resp.StatusCode, body)
			}
			if got := tokens(e.one(t)); got != tc.want {
				t.Fatalf("tokens in/out/cacheRead/cacheCreation = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRecordStoresIdentitySnapshotsAndTimings(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, ok(`{}`))
	e.route([]string{"a"})
	key, token := e.newKey(t)
	e.token = token
	if resp, body := e.chat(t, simple); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	r := e.one(t)
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	if r.ProxyKeyID != key.ID || r.ProxyKeyName != key.Name || r.VirtualModel != "m" || str(r.ProviderID) != "a" || str(r.ProviderName) != "a" ||
		str(r.KeyID) != "k-a" || str(r.KeyName) != "key-a" || str(r.Model) != "real-a" || r.Status != 200 || r.Steps != 1 || r.Affinity != nil || r.DurationMs < 0 {
		t.Fatalf("%+v", r)
	}
	if tokens(r) != [4]int{-1, -1, -1, -1} {
		t.Fatalf("a body without usage must store unknown tokens: %v", tokens(r))
	}
}

func TestStreamRecordedOnlyAfterItCloses(t *testing.T) {
	e := newEnv(t)
	finish := make(chan struct{})
	e.upstream(t, "a", 1, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "data: {\"n\":1}\n\n")
		<-finish
		flushed(w, "data: {\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	})
	e.route([]string{"a"})
	_, body := e.open(t, context.Background())
	buf := make([]byte, 5)
	if _, err := body.Read(buf); err != nil {
		t.Fatal(err)
	}
	if n := len(e.rec.all()); n != 0 {
		t.Fatalf("recorded %d before the stream ended", n)
	}
	close(finish)
	buf = make([]byte, 1<<10)
	for {
		if _, err := body.Read(buf); err != nil {
			break
		}
	}
	if r := e.one(t); r.Status != 200 || tokens(r) != [4]int{4, 1, 0, -1} {
		t.Fatalf("%+v", r)
	}
}

func TestStreamClientDisconnectRecordsOnceWithCommittedStatus(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "data: {\"n\":1}\n\n")
		<-r.Context().Done()
	})
	e.route([]string{"a"})
	ctx, cancel := context.WithCancel(context.Background())
	_, body := e.open(t, ctx)
	_, _ = body.Read(make([]byte, 5))
	cancel()
	if r := e.one(t); r.Status != 200 {
		t.Fatalf("%+v", r)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(e.rec.all()); n != 1 {
		t.Fatalf("want exactly one record, got %d", n)
	}
}

func TestFailoverRecordsStepsAndFinalTarget(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, status(http.StatusBadGateway))
	e.upstream(t, "b", 1, ok(`{}`))
	e.route([]string{"a", "b"})
	e.chat(t, simple)
	if r := e.one(t); r.Steps != 2 || *r.ProviderID != "b" || *r.KeyName != "key-b" || r.Status != 200 {
		t.Fatalf("%+v", r)
	}
}

func TestEngineErrorsAreRecordedWithStatus(t *testing.T) {
	t.Run("all upstreams fail", func(t *testing.T) {
		e := newEnv(t)
		e.upstream(t, "a", 1, status(http.StatusBadGateway))
		e.route([]string{"a"})
		e.chat(t, simple)
		if r := e.one(t); r.Status != 502 || r.Steps != 1 || *r.ProviderID != "a" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("no usable provider", func(t *testing.T) {
		e := newEnv(t)
		e.upstream(t, "a", 1, ok(`{}`))
		p := e.provs["a"]
		p.Enabled = false
		e.provs["a"] = p
		e.route([]string{"a"})
		e.chat(t, simple)
		if r := e.one(t); r.Status != 503 || r.Steps != 0 || r.ProviderID != nil {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("busy chain", func(t *testing.T) {
		e := newEnv(t)
		b := newBlocker()
		e.upstream(t, "a", 1, b.handlerFunc)
		e.route([]string{"a"})
		first := e.chatAsync(t)
		<-b.arrived
		e.chat(t, simple)
		close(b.release)
		<-first
		if recs := e.rec.wait(t, 2); recs[0].Status != 429 || recs[0].Steps != 0 || recs[1].Status != 200 {
			t.Fatalf("%+v", recs)
		}
	})
}

func TestClientCancelIsRecordedAs499(t *testing.T) {
	e := newEnv(t)
	b := newBlocker()
	defer close(b.release)
	e.upstream(t, "a", 1, b.handlerFunc)
	e.route([]string{"a"})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.srv.URL+"/v1/chat/completions", strings.NewReader(simple))
	req.Header.Set("Authorization", "Bearer "+e.token)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
		t.Fatal("request should have been cancelled")
	}
	if r := e.one(t); r.Status != 499 {
		t.Fatalf("%+v", r)
	}
}

func TestRejectedBeforeTheEngineIsNotRecorded(t *testing.T) {
	e := newEnv(t)
	e.settings.s.MaxBodyBytes = 200
	e.upstream(t, "a", 1, ok(`{}`))
	e.route([]string{"a"})
	_, limited := e.newKey(t, "other")
	e.do(t, http.MethodPost, "/v1/chat/completions", simple, map[string]string{"Authorization": "Bearer nope"})
	e.chat(t, `{"messages":[]}`)
	e.chat(t, `{"model":"m","pad":"`+strings.Repeat("x", 300)+`"}`)
	e.do(t, http.MethodPost, "/v1/chat/completions", simple, map[string]string{"Authorization": "Bearer " + limited})
	if n := len(e.rec.all()); n != 0 {
		t.Fatalf("%d rejected requests were recorded: %+v", n, e.rec.all())
	}
}

func TestTranscriptionIsRecordedWithoutTokens(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, ok(`{"text":"hi","usage":{"prompt_tokens":9}}`))
	e.routeKind(virtualmodels.KindTranscription, []string{"a"})
	body, ctype := multipartBody(t, field{"model", "", "", "m"}, field{"file", "a.wav", "audio/wav", "RIFF"})
	if resp, out := e.transcribe(t, body, ctype); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, out)
	}
	if r := e.one(t); tokens(r) != [4]int{-1, -1, -1, -1} || *r.ProviderID != "a" {
		t.Fatalf("%+v", r)
	}
}

func TestAffinityOutcomeIsRecorded(t *testing.T) {
	e := newEnv(t)
	broken := false
	e.upstream(t, "a", 9, func(w http.ResponseWriter, r *http.Request) {
		if broken {
			status(http.StatusBadGateway)(w, r)
			return
		}
		ok(`{}`)(w, r)
	})
	e.upstream(t, "b", 9, ok(`{}`))
	e.affine("a", "b")
	e.turns(t, 2, "conv-1")
	broken = true
	e.turns(t, 1, "conv-1")
	recs := e.rec.wait(t, 3)
	var got []string
	for _, r := range recs {
		got = append(got, *r.Affinity)
	}
	if strings.Join(got, ",") != "new,hit,break" || recs[2].Steps != 2 || *recs[2].ProviderID != "b" {
		t.Fatalf("%v %+v", got, recs[2])
	}
}
