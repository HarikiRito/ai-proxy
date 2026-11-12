package gateway

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const streamReq = `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`

func flushed(w http.ResponseWriter, s string) {
	_, _ = w.Write([]byte(s))
	_ = http.NewResponseController(w).Flush()
}

func sse(chunks ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			flushed(w, c)
		}
	}
}

// open starts a streaming chat and returns once the response headers arrived.
func (e *env) open(t *testing.T, ctx context.Context) (reply, io.Reader) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.srv.URL+"/v1/chat/completions", strings.NewReader(streamReq))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return reply{resp.StatusCode, resp.Header}, resp.Body
}

func TestStreamFlushesIncrementallyAndPassesBytesThrough(t *testing.T) {
	e := newEnv(t)
	finish := make(chan struct{})
	var gotBody []byte
	var gotAuth string
	e.upstream(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "data: {\"n\":1}\n\n")
		<-finish
		flushed(w, "data: {\"n\":2}\n\ndata: [DONE]\n\n")
	})
	e.route([]string{"a"})

	resp, rbody := e.open(t, context.Background())
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers: %d %v", resp.StatusCode, resp.Header)
	}
	br := bufio.NewReader(rbody)
	first, err := br.ReadString('\n')
	if err != nil || first != "data: {\"n\":1}\n" {
		t.Fatalf("first chunk %q %v", first, err)
	}
	close(finish)
	rest, _ := io.ReadAll(br)
	if first+string(rest) != "data: {\"n\":1}\n\ndata: {\"n\":2}\n\ndata: [DONE]\n\n" {
		t.Fatalf("bytes changed: %q", first+string(rest))
	}
	if gotAuth != "Bearer real-k-a" || !strings.Contains(string(gotBody), `"model":"real-a"`) {
		t.Fatalf("upstream saw auth=%q body=%s", gotAuth, gotBody)
	}
}

func TestStreamFailoverBeforeCommit(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{"429": status(http.StatusTooManyRequests), "5xx": status(http.StatusBadGateway)} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			a := e.upstream(t, "a", 1, h)
			e.upstream(t, "b", 1, sse("data: from-b\n\n"))
			e.route([]string{"a", "b"})
			for range 2 {
				if _, body := e.chat(t, streamReq); body != "data: from-b\n\n" {
					t.Fatalf("%q", body)
				}
			}
			if want := map[string]int32{"429": 1, "5xx": 2}[name]; a.Load() != want {
				t.Fatalf("a hit %d times, want %d", a.Load(), want)
			}
		})
	}
}

func TestStreamClientErrorPassesThrough(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, status(http.StatusUnauthorized))
	b := e.upstream(t, "b", 1, sse("data: x\n\n"))
	e.route([]string{"a", "b"})
	if resp, body := e.chat(t, streamReq); resp.StatusCode != 401 || body != `{"error":"upstream says no"}` || b.Load() != 0 {
		t.Fatalf("%d %s b=%d", resp.StatusCode, body, b.Load())
	}
}

// onceThen runs first for the first call and then for every later one.
func onceThen(first, then http.HandlerFunc) http.HandlerFunc {
	var n atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			first(w, r)
			return
		}
		then(w, r)
	}
}

func TestStreamUpstreamBreakSendsErrorEventAndFreesLease(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, onceThen(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "data: partial\n\n")
		c, _, _ := w.(http.Hijacker).Hijack()
		_ = c.Close()
	}, ok(`{"ok":true}`)))
	e.route([]string{"a"})

	_, body := e.chat(t, streamReq)
	want := "data: partial\n\ndata: {\"error\":{\"code\":\"stream_interrupted\",\"message\":\"upstream stream interrupted\",\"type\":\"upstream_error\"}}\n\n"
	if body != want {
		t.Fatalf("got %q", body)
	}
	if _, body := e.chat(t, simple); body != `{"ok":true}` {
		t.Fatalf("lease not freed: %s", body)
	}
}

func TestStreamClientDisconnectCancelsUpstreamAndFreesLease(t *testing.T) {
	e := newEnv(t)
	cancelled := make(chan struct{})
	e.upstream(t, "a", 1, onceThen(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "data: hi\n\n")
		<-r.Context().Done()
		close(cancelled)
	}, ok(`{"ok":true}`)))
	e.route([]string{"a"})

	ctx, cancel := context.WithCancel(context.Background())
	_, rbody := e.open(t, ctx)
	if _, err := bufio.NewReader(rbody).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}
	if _, body := e.chat(t, simple); body != `{"ok":true}` {
		t.Fatalf("lease not freed: %s", body)
	}
}

func TestStreamTimeoutMidStreamSendsTimeoutEvent(t *testing.T) {
	e := newEnv(t)
	e.settings.s.RequestTimeoutSeconds = 1
	e.upstream(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flushed(w, "data: slow\n\n")
		<-r.Context().Done()
	})
	e.route([]string{"a"})
	_, body := e.chat(t, streamReq)
	if !strings.HasPrefix(body, "data: slow\n\ndata: {") || !strings.Contains(body, `"code":"timeout"`) || !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("got %q", body)
	}
}

func TestHeaderForwarding(t *testing.T) {
	for _, stream := range []bool{false, true} {
		e := newEnv(t)
		var got http.Header
		var gotHost string
		e.upstream(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
			got, gotHost = r.Header.Clone(), r.Host
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: x\n\n"))
		})
		e.route([]string{"a"})
		body := simple
		if stream {
			body = streamReq
		}
		e.do(t, http.MethodPost, "/v1/chat/completions", body, map[string]string{
			"Authorization": "Bearer " + e.token, "X-Api-Key": "proxy-secret", "Cookie": "s=1",
			"Accept-Encoding": "br", "Connection": "keep-alive, X-Hop", "X-Hop": "1", "Keep-Alive": "timeout=5",
			"Proxy-Authorization": "p", "Te": "trailers", "Upgrade": "websocket", "Trailer": "X-T",
			"OpenAI-Organization": "org-1", "X-Custom": "c", "Accept": "text/event-stream",
		})
		for _, h := range []string{"X-Api-Key", "Cookie", "X-Hop", "Keep-Alive", "Proxy-Authorization", "Te", "Upgrade", "Trailer"} {
			if got.Get(h) != "" {
				t.Errorf("stream=%v: %s was forwarded", stream, h)
			}
		}
		if got.Get("Authorization") != "Bearer real-k-a" || got.Get("Accept-Encoding") == "br" || gotHost == "" {
			t.Errorf("stream=%v: auth=%q enc=%q", stream, got.Get("Authorization"), got.Get("Accept-Encoding"))
		}
		if got.Get("OpenAI-Organization") != "org-1" || got.Get("X-Custom") != "c" || got.Get("Accept") != "text/event-stream" || got.Get("Content-Type") != "application/json" {
			t.Errorf("stream=%v: custom headers lost: %v", stream, got)
		}
	}
}

func TestImagePartsPassThroughUnchanged(t *testing.T) {
	const parts = `[{"type":"text","text":"a <b>"},{"type":"image_url","image_url":{"url":"https://example.com/a.png?x=1&y=2","detail":"high"}},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]`
	for _, stream := range []string{"false", "true"} {
		e := newEnv(t)
		var seen []byte
		e.upstream(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
			seen, _ = io.ReadAll(r.Body)
			sse("data: x\n\n")(w, r)
		})
		e.route([]string{"a"})
		e.chat(t, `{"model":"m","stream":`+stream+`,"messages":[{"role":"user","content":`+parts+`}]}`)
		if !bytes.Contains(seen, []byte(parts)) {
			t.Fatalf("stream=%s: parts changed: %s", stream, seen)
		}
	}
}
