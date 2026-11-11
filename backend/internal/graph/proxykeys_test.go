package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/proxykeys"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

func proxyKeysHandler(t *testing.T) http.Handler {
	t.Helper()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return NewHandler(&Resolver{ProxyKeys: proxykeys.New(proxykeys.Deps{DB: client})}, false)
}

func TestProxyKeyLifecycleRevealsSecretOnce(t *testing.T) {
	h := proxyKeysHandler(t)
	out := gql(t, h, `mutation($i: CreateProxyKeyInput!) { createProxyKey(input: $i) { secret key { id name status allowedModels lastUsedAt } } }`,
		map[string]any{"i": map[string]any{"name": "ci", "allowedModels": []string{"fast"}}})
	var created struct {
		Data struct {
			CreateProxyKey struct {
				Secret string
				Key    struct {
					ID, Name, Status string
					AllowedModels    []string
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("create: %s", out)
	}
	c := created.Data.CreateProxyKey
	if !strings.HasPrefix(c.Secret, proxykeys.Prefix) || c.Key.Status != "active" || c.Key.Name != "ci" || len(c.Key.AllowedModels) != 1 {
		t.Fatalf("create: %s", out)
	}

	out = gql(t, h, `{ proxyKeys { id name status allowedModels createdAt lastUsedAt revokedAt } }`, nil)
	if strings.Contains(out, c.Secret) || !strings.Contains(out, c.Key.ID) || !strings.Contains(out, `"lastUsedAt":null`) {
		t.Fatalf("list: %s", out)
	}

	out = gql(t, h, `mutation($id: ID!) { setProxyKeyAllowedModels(id: $id, models: []) { allowedModels } }`, map[string]any{"id": c.Key.ID})
	if !strings.Contains(out, `"allowedModels":[]`) {
		t.Fatalf("set models: %s", out)
	}

	out = gql(t, h, `mutation($id: ID!) { revokeProxyKey(id: $id) { status revokedAt } }`, map[string]any{"id": c.Key.ID})
	if !strings.Contains(out, `"status":"revoked"`) || strings.Contains(out, `"revokedAt":null`) {
		t.Fatalf("revoke: %s", out)
	}
}

func TestProxyKeyUserErrorsAreShown(t *testing.T) {
	h := proxyKeysHandler(t)
	out := gql(t, h, `mutation { createProxyKey(input: {name: " "}) { secret } }`, nil)
	if !strings.Contains(out, "name is required") {
		t.Fatalf("message hidden: %s", out)
	}
}
