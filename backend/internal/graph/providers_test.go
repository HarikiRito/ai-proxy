package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/crypto"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

const secret = "sk-DISTINCTIVE-secret-abcd1234"

func providersHandler(t *testing.T) http.Handler {
	t.Helper()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	cipher, err := crypto.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(&Resolver{Providers: providers.New(providers.Deps{DB: client, Cipher: cipher})}, false)
}

func gql(t *testing.T, h http.Handler, query string, vars map[string]any) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestProviderLifecycleNeverReturnsSecret(t *testing.T) {
	h := providersHandler(t)
	out := gql(t, h, `mutation($i: ProviderInput!) { createProvider(input: $i) { id kind capacity models } }`,
		map[string]any{"i": map[string]any{"name": "openai", "kind": "openai_compatible", "baseUrl": "https://api.openai.com/v1", "models": []string{"gpt-4o"}}})
	var created struct {
		Data struct{ CreateProvider struct{ ID string } }
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.Data.CreateProvider.ID == "" {
		t.Fatalf("create: %s", out)
	}

	out = gql(t, h, `mutation($p: ID!, $i: ProviderKeyInput!) { addProviderKey(providerId: $p, input: $i) { name maskedKey maxInFlight } }`,
		map[string]any{"p": created.Data.CreateProvider.ID, "i": map[string]any{"name": "main", "secret": secret, "maxInFlight": 5}})
	if !strings.Contains(out, `"maskedKey":"sk-DIS…1234"`) || strings.Contains(out, secret) {
		t.Fatalf("add key: %s", out)
	}

	out = gql(t, h, `{ providers { name capacity models keys { name maskedKey } } }`, nil)
	if !strings.Contains(out, `"models":["gpt-4o"]`) || !strings.Contains(out, `"capacity":5`) || !strings.Contains(out, `"maskedKey":"sk-DIS…1234"`) || strings.Contains(out, secret) {
		t.Fatalf("list: %s", out)
	}
}

func TestSchemaExposesNoSecretField(t *testing.T) {
	schema := NewExecutableSchema(Config{}).Schema()
	for name, def := range schema.Types {
		if strings.HasPrefix(name, "__") || def.Kind == "INPUT_OBJECT" {
			continue
		}
		for _, f := range def.Fields {
			if name == "CreatedProxyKey" && f.Name == "secret" { // the one-time reveal of a new proxy key
				continue
			}
			n := strings.ToLower(f.Name)
			if strings.Contains(n, "secret") || strings.Contains(n, "ciphertext") || strings.Contains(n, "hash") {
				t.Errorf("%s.%s exposes a secret", name, f.Name)
			}
		}
	}
	if schema.Types["ProviderKeyInput"].Fields.ForName("secret") == nil {
		t.Error("ProviderKeyInput.secret must exist")
	}
}

func TestValidationErrorsAreShownAndInternalOnesHidden(t *testing.T) {
	h := providersHandler(t)
	out := gql(t, h, `mutation { createProvider(input: {name: "x", kind: openai_compatible, baseUrl: "nope"}) { id } }`, nil)
	if !strings.Contains(out, "base URL must be an http(s) URL") {
		t.Fatalf("validation message hidden: %s", out)
	}
	if got := presentError(context.Background(), errInternal{}); got.Message != "internal server error" {
		t.Fatalf("internal error leaked: %s", got.Message)
	}
}

type errInternal struct{}

func (errInternal) Error() string { return "sqlite: disk on fire" }

func TestSubscriptionAccountsOverGraphQL(t *testing.T) {
	const setupToken = "sk-ant-oat01-DISTINCTIVE-token-abcdef123456"
	h := providersHandler(t)
	out := gql(t, h, `mutation { createProvider(input: {name: "sub", kind: claude_subscription, baseUrl: "http://sidecar:3000"}) { id } }`, nil)
	var created struct {
		Data struct{ CreateProvider struct{ ID string } }
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.Data.CreateProvider.ID == "" {
		t.Fatalf("create: %s", out)
	}
	add := func(in map[string]any) string {
		in["maxInFlight"] = 1
		return gql(t, h, `mutation($p: ID!, $i: ProviderKeyInput!) { addProviderKey(providerId: $p, input: $i) { name maskedKey authMode tokenCreatedAt tokenExpiresAt } }`,
			map[string]any{"p": created.Data.CreateProvider.ID, "i": in})
	}

	out = add(map[string]any{"name": "tok", "secret": setupToken, "authMode": "SETUP_TOKEN", "tokenCreatedAt": "2026-01-01T00:00:00Z"})
	if !strings.Contains(out, `"authMode":"SETUP_TOKEN"`) || !strings.Contains(out, `"tokenExpiresAt":"2027-01-01T00:00:00Z"`) || strings.Contains(out, setupToken) {
		t.Fatalf("token account: %s", out)
	}
	out = add(map[string]any{"name": "dir", "secret": "acct-1", "authMode": "LOGIN_DIR"})
	if !strings.Contains(out, `"maskedKey":"acct-1"`) || !strings.Contains(out, `"tokenExpiresAt":null`) {
		t.Fatalf("dir account: %s", out)
	}
	for name, in := range map[string]map[string]any{
		"api_key on subscription": {"name": "x", "secret": setupToken, "authMode": "API_KEY"},
		"missing mode":            {"name": "x", "secret": setupToken},
		"bad dir":                 {"name": "x", "secret": "../etc", "authMode": "LOGIN_DIR"},
	} {
		if out := add(in); !strings.Contains(out, `"errors"`) || strings.Contains(out, "internal server error") || strings.Contains(out, setupToken) {
			t.Fatalf("%s: %s", name, out)
		}
	}

	out = gql(t, h, `{ providers { keys { name authMode maskedKey tokenCreatedAt tokenExpiresAt } } }`, nil)
	if strings.Contains(out, setupToken) || !strings.Contains(out, `"authMode":"LOGIN_DIR"`) {
		t.Fatalf("list: %s", out)
	}
}
