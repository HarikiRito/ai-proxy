// Package httpx builds the chi router with the shared middleware stack.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Options struct {
	Log            *slog.Logger
	AllowedOrigins []string
	// Ready backs GET /healthz; a non-nil error answers 503.
	Ready func(context.Context) error
	// Static is the SPA served for every non-API path.
	Static fs.FS
	// WorkOSClientID and DevMode are served publicly at GET /config.json for the SPA.
	WorkOSClientID string
	DevMode        bool
}

func NewRouter(o Options) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, accessLog(o.Log))
	if len(o.AllowedOrigins) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins: o.AllowedOrigins,
			AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
			AllowedHeaders: []string{"Authorization", "Content-Type"},
		}))
	}
	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		status, body := http.StatusOK, `{"status":"ok","db":"ok"}`
		if err := o.Ready(req.Context()); err != nil {
			status, body = http.StatusServiceUnavailable, `{"status":"unavailable","db":"error"}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	r.Get("/config.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"workosClientId": o.WorkOSClientID, "devMode": o.DevMode})
	})
	if o.Static != nil {
		r.NotFound(spaHandler(o.Static))
	}
	return r
}

func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.InfoContext(r.Context(), "request", "method", r.Method, "path", r.URL.Path,
				"status", ww.Status(), "bytes", ww.BytesWritten(), "duration", time.Since(start),
				"request_id", middleware.GetReqID(r.Context()))
		})
	}
}

// spaHandler serves the file at the request path, else 200.html so client-side deep links load.
// The fallback applies to GET/HEAD page routes only: other methods, /_app/ assets and
// paths with a file extension that are missing answer 404.
// It avoids index.html as the fallback because the build references assets by absolute /_app/ paths.
func spaHandler(static fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(static, name); err != nil || info.IsDir() {
			if strings.HasPrefix(name, "_app/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "200.html"
		}
		data, err := fs.ReadFile(static, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	}
}
