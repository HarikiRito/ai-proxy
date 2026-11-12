package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

type field struct{ name, filename, ctype, data string }

func multipartBody(t *testing.T, fields ...field) (string, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, f := range fields {
		h := textproto.MIMEHeader{}
		disp := `form-data; name="` + f.name + `"`
		if f.filename != "" {
			disp += `; filename="` + f.filename + `"`
		}
		h.Set("Content-Disposition", disp)
		if f.ctype != "" {
			h.Set("Content-Type", f.ctype)
		}
		pw, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = pw.Write([]byte(f.data))
	}
	_ = w.Close()
	return b.String(), w.FormDataContentType()
}

func readForm(r *http.Request) map[string]field {
	_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	got := map[string]field{}
	mr := multipart.NewReader(r.Body, params["boundary"])
	for p, err := mr.NextPart(); err == nil; p, err = mr.NextPart() {
		data, _ := io.ReadAll(p)
		got[p.FormName()] = field{p.FormName(), p.FileName(), p.Header.Get("Content-Type"), string(data)}
	}
	return got
}

func (e *env) transcribe(t *testing.T, body, contentType string) (reply, string) {
	t.Helper()
	return e.do(t, http.MethodPost, "/v1/audio/transcriptions", body, map[string]string{"Authorization": "Bearer " + e.token, "Content-Type": contentType})
}

func (e *env) audio(t *testing.T, tiers ...[]string) {
	t.Helper()
	e.routeKind(virtualmodels.KindTranscription, tiers...)
}

func sampleForm(t *testing.T, audio string) (string, string) {
	return multipartBody(t,
		field{"model", "", "", "m"},
		field{"language", "", "", "en"},
		field{"file", "clip.mp3", "audio/mpeg", audio},
	)
}

func TestTranscriptionRewritesModelOnlyAndKeepsFile(t *testing.T) {
	e := newEnv(t)
	audio := string(bytes.Repeat([]byte{0, 1, 2, '\r', '\n', '-', '-', 255}, 5000))
	var got map[string]field
	var gotAuth, gotPath, gotType string
	e.upstream(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotType = r.Header.Get("Authorization"), r.URL.Path, r.Header.Get("Content-Type")
		got = readForm(r)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	})
	e.audio(t, []string{"a"})
	body, ctype := sampleForm(t, audio)
	resp, out := e.transcribe(t, body, ctype)

	if resp.StatusCode != 200 || out != "hello world" || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("response not passed through: %d %q %q", resp.StatusCode, out, resp.Header.Get("Content-Type"))
	}
	_, clientParams, _ := mime.ParseMediaType(ctype)
	_, upParams, err := mime.ParseMediaType(gotType)
	if err != nil || upParams["boundary"] == "" || upParams["boundary"] == clientParams["boundary"] {
		t.Fatalf("upstream content type %q must carry its own boundary, client had %q", gotType, clientParams["boundary"])
	}
	if gotAuth != "Bearer real-k-a" || gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("auth=%q path=%q", gotAuth, gotPath)
	}
	file := got["file"]
	if sha256.Sum256([]byte(file.data)) != sha256.Sum256([]byte(audio)) || file.filename != "clip.mp3" || file.ctype != "audio/mpeg" {
		t.Fatalf("file changed: name=%q type=%q len=%d", file.filename, file.ctype, len(file.data))
	}
	if got["model"].data != "real-a" || got["language"].data != "en" || len(got) != 3 {
		t.Fatalf("fields: %+v", got)
	}
}

func TestTranscriptionFailoverReplaysBody(t *testing.T) {
	t.Run("429 cools the key", func(t *testing.T) { replayAfter(t, status(http.StatusTooManyRequests), 1) })
	t.Run("5xx does not", func(t *testing.T) { replayAfter(t, status(http.StatusBadGateway), 2) })
}

func replayAfter(t *testing.T, failing http.HandlerFunc, wantHits int32) {
	e := newEnv(t)
	audio := strings.Repeat("abc\x00\xff", 4000)
	a := e.upstream(t, "a", 1, failing)
	var got map[string]field
	e.upstream(t, "b", 1, func(w http.ResponseWriter, r *http.Request) {
		got = readForm(r)
		ok(`{"text":"b"}`)(w, r)
	})
	e.audio(t, []string{"a", "b"})
	body, ctype := sampleForm(t, audio)
	for range 2 {
		if _, out := e.transcribe(t, body, ctype); out != `{"text":"b"}` {
			t.Fatal(out)
		}
	}
	if sha256.Sum256([]byte(got["file"].data)) != sha256.Sum256([]byte(audio)) || got["model"].data != "real-b" {
		t.Fatalf("replayed body corrupted: model=%q", got["model"].data)
	}
	if a.Load() != wantHits {
		t.Fatalf("a hit %d times, want %d", a.Load(), wantHits)
	}
}

func TestTranscriptionBodyLimit(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, ok(`{"text":"x"}`))
	e.audio(t, []string{"a"})
	body, ctype := sampleForm(t, strings.Repeat("x", 500))
	e.settings.s.MaxBodyBytes = len(body)
	if resp, out := e.transcribe(t, body, ctype); resp.StatusCode != 200 {
		t.Fatalf("at limit: %d %s", resp.StatusCode, out)
	}
	e.settings.s.MaxBodyBytes = len(body) - 1
	resp, out := e.transcribe(t, body, ctype)
	wantError(t, resp, out, http.StatusRequestEntityTooLarge, "request_too_large")
}

func TestTranscriptionValidation(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, ok(`{"text":"x"}`))
	e.audio(t, []string{"a"})
	e.models["chatty"] = virtualmodels.Model{Name: "chatty", Kind: virtualmodels.KindChat}
	_, only := e.newKey(t, "chatty", "m")
	e.models["other"] = virtualmodels.Model{Name: "other", Kind: virtualmodels.KindTranscription}

	manyParts, mpctype := multipartBody(t, make([]field, 100)...)
	form := func(fields ...field) (string, string) {
		return multipartBody(t, append(fields, field{"file", "a.mp3", "audio/mpeg", "x"})...)
	}
	forbidden, fctype := form(field{"model", "", "", "other"})
	missing, mctype := form(field{"language", "", "", "en"})
	unknown, uctype := form(field{"model", "", "", "nope"})
	wrongKind, wctype := form(field{"model", "", "", "chatty"})
	streaming, sctype := form(field{"model", "", "", "m"}, field{"stream", "", "", "true"})
	twoStreams, tsctype := form(field{"model", "", "", "m"}, field{"stream", "", "", "false"}, field{"stream", "", "", "true"})
	_, plain := form(field{"model", "", "", "m"})

	for name, tc := range map[string]struct {
		token, body, ctype string
		status             int
		code               string
	}{
		"unknown model":    {"", unknown, uctype, 404, "model_not_found"},
		"chat model":       {"", wrongKind, wctype, 400, "wrong_model_kind"},
		"not allowed":      {only, forbidden, fctype, 403, "model_not_allowed"},
		"missing model":    {"", missing, mctype, 400, "invalid_request"},
		"stream":           {"", streaming, sctype, 400, "invalid_request"},
		"second stream":    {"", twoStreams, tsctype, 400, "invalid_request"},
		"too many parts":   {"", manyParts, mpctype, 400, "invalid_request"},
		"json body":        {"", `{"model":"m"}`, "application/json", 400, "invalid_request"},
		"missing boundary": {"", "x", "multipart/form-data", 400, "invalid_request"},
		"wrong boundary":   {"", missing, strings.Replace(plain, "boundary=", "boundary=zz", 1), 400, "invalid_request"},
		"truncated":        {"", missing[:len(missing)/2], mctype, 400, "invalid_request"},
	} {
		t.Run(name, func(t *testing.T) {
			token := tc.token
			if token == "" {
				token = e.token
			}
			resp, out := e.do(t, http.MethodPost, "/v1/audio/transcriptions", tc.body, map[string]string{"Authorization": "Bearer " + token, "Content-Type": tc.ctype})
			wantError(t, resp, out, tc.status, tc.code)
		})
	}
}

func TestKindGateIsStrictBothWays(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 1, ok("{}"))
	e.audio(t, []string{"a"})
	resp, out := e.chat(t, simple)
	wantError(t, resp, out, http.StatusBadRequest, "wrong_model_kind")
}

func TestTranscriptionSkipsNonOpenAITargets(t *testing.T) {
	e := newEnv(t)
	hits := e.upstream(t, "a", 1, ok("{}"))
	p := e.provs["a"]
	p.Kind = providers.KindAnthropic
	e.provs["a"] = p
	e.upstream(t, "b", 1, ok(`{"text":"b"}`))
	e.audio(t, []string{"a"}, []string{"b"})
	body, ctype := sampleForm(t, "x")
	if _, out := e.transcribe(t, body, ctype); out != `{"text":"b"}` || hits.Load() != 0 {
		t.Fatalf("%s, anthropic hit %d", out, hits.Load())
	}
}

func TestTranscriptionClientCancelFreesLease(t *testing.T) {
	e := newEnv(t)
	cancelled := make(chan struct{})
	arrived := make(chan struct{}, 2)
	e.upstream(t, "a", 1, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hang") != "" {
			_, _ = io.Copy(io.Discard, r.Body) // the server only notices a closed connection after the body is read
			arrived <- struct{}{}
			<-r.Context().Done()
			close(cancelled)
			return
		}
		ok(`{"text":"ok"}`)(w, r)
	})
	e.audio(t, []string{"a"})
	body, ctype := sampleForm(t, "x")

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.srv.URL+"/v1/audio/transcriptions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("X-Hang", "1")
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-arrived
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, out := e.transcribe(t, body, ctype); out == `{"text":"ok"}` {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("lease not freed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
