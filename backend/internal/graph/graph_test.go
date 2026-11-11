package graph

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/health"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/auth"

	_ "modernc.org/sqlite"
)

func query(t *testing.T, pinger health.Pinger) string {
	t.Helper()
	h := NewHandler(&Resolver{Health: health.New(health.Deps{DB: pinger})}, false)
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ health { ok } }"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestHealthQuery(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/app.db")
	if err != nil {
		t.Fatal(err)
	}
	if got := query(t, conn); got != `{"data":{"health":{"ok":true}}}` {
		t.Fatalf("healthy: %s", got)
	}
	_ = conn.Close()
	if got := query(t, conn); got != `{"data":{"health":{"ok":false}}}` {
		t.Fatalf("closed: %s", got)
	}
}

func TestViewerQueryReturnsOwnerEmailFromContext(t *testing.T) {
	h := NewHandler(&Resolver{}, false)
	run := func(ctx context.Context) string {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ viewer { email } }"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	if got := run(auth.WithOwnerEmail(context.Background(), "owner@example.com")); got != `{"data":{"viewer":{"email":"owner@example.com"}}}` {
		t.Fatalf("with owner: %s", got)
	}
	if got := run(context.Background()); strings.Contains(got, "owner@") || !strings.Contains(got, `"errors"`) {
		t.Fatalf("without owner must error: %s", got)
	}
}

func TestSchemaHasNoAuthOperations(t *testing.T) {
	schema := NewExecutableSchema(Config{}).Schema()
	for _, name := range []string{"register", "login", "me"} {
		if schema.Query.Fields.ForName(name) != nil {
			t.Errorf("Query.%s must not exist", name)
		}
	}
	if schema.Query.Fields.ForName("viewer") == nil {
		t.Error("Query.viewer must exist")
	}
	if schema.Mutation != nil {
		t.Error("no mutations expected")
	}
}
