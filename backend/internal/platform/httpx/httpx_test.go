package httpx

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"
)

var spa = fstest.MapFS{
	"index.html":   {Data: []byte("<html>index</html>")},
	"200.html":     {Data: []byte("<html>fallback</html>")},
	"_app/main.js": {Data: []byte("console.log(1)")},
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func newTestRouter(ready func(context.Context) error) http.Handler {
	r := NewRouter(Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Ready: ready, Static: spa})
	r.Post("/graphql", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("gql")) })
	return r
}

func TestStaticServing(t *testing.T) {
	h := newTestRouter(func(context.Context) error { return nil })
	tests := []struct {
		path, body, contentType string
	}{
		{"/", "index", "text/html"},
		{"/deep/route", "fallback", "text/html"},
		{"/_app/main.js", "console.log(1)", "javascript"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := get(t, h, tt.path)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tt.body) ||
				!strings.Contains(rec.Header().Get("Content-Type"), tt.contentType) {
				t.Fatalf("got %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
			}
		})
	}
}

func TestFallbackDoesNotMaskMissingAssetsOrWrongMethods(t *testing.T) {
	h := newTestRouter(func(context.Context) error { return nil })

	for _, path := range []string{"/_app/immutable/x.js", "/nope.txt"} {
		if rec := get(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: got %d, want 404", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /nope: got %d, want 404", rec.Code)
	}
}

func TestAPIRoutesWinOverFallback(t *testing.T) {
	h := newTestRouter(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/graphql", nil))
	if rec.Body.String() != "gql" {
		t.Fatalf("graphql shadowed by fallback: %q", rec.Body.String())
	}
}

func TestHealthzReportsDatabase(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/app.db")
	if err != nil {
		t.Fatal(err)
	}
	h := newTestRouter(conn.PingContext)

	rec := get(t, h, "/healthz")
	var body map[string]string
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["db"] != "ok" {
		t.Fatalf("healthy: got %d %q", rec.Code, rec.Body.String())
	}

	_ = conn.Close()
	rec = get(t, h, "/healthz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed db: got %d", rec.Code)
	}
}
