package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

type directory map[string]virtualmodels.ProviderInfo

func (d directory) ProviderInfo(_ context.Context, ids []string) (map[string]virtualmodels.ProviderInfo, error) {
	out := map[string]virtualmodels.ProviderInfo{}
	for _, id := range ids {
		if info, ok := d[id]; ok {
			out[id] = info
		}
	}
	return out, nil
}

func virtualModelsHandler(t *testing.T, dir directory) http.Handler {
	t.Helper()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	svc := virtualmodels.New(virtualmodels.Deps{DB: client, Providers: dir})
	return NewHandler(&Resolver{VirtualModels: svc}, false)
}

const modelFields = `id name kind strategy cacheAffinity tiers { priority entries { id providerId providerName model weight status } }`

func TestVirtualModelRoundTrip(t *testing.T) {
	pid := uuid.NewString()
	dir := directory{pid: {Name: "openai", Kind: "openai_compatible", Enabled: true}}
	h := virtualModelsHandler(t, dir)
	entry := func(model string, weight int) map[string]any {
		return map[string]any{"providerId": pid, "model": model, "weight": weight}
	}

	out := gql(t, h, `mutation($i: CreateVirtualModelInput!) { createVirtualModel(input: $i) { `+modelFields+` } }`, map[string]any{"i": map[string]any{
		"name": "smart", "kind": "chat", "strategy": "weighted_round_robin", "cacheAffinity": false,
		"tiers": []any{map[string]any{"entries": []any{entry("gpt-5", 3)}}, map[string]any{"entries": []any{entry("gpt-mini", 1)}}},
	}})
	var created struct {
		Data struct{ CreateVirtualModel struct{ ID string } }
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.Data.CreateVirtualModel.ID == "" {
		t.Fatalf("create: %s", out)
	}
	id := created.Data.CreateVirtualModel.ID

	out = gql(t, h, `{ virtualModels { `+modelFields+` } }`, nil)
	for _, want := range []string{`"strategy":"weighted_round_robin"`, `"cacheAffinity":false`, `"status":"ok"`, `"providerName":"openai"`, `"weight":3`, `"priority":1`} {
		if !strings.Contains(out, want) {
			t.Fatalf("list missing %s: %s", want, out)
		}
	}

	out = gql(t, h, `mutation($id: ID!, $i: UpdateVirtualModelInput!) { updateVirtualModel(id: $id, input: $i) { name strategy tiers { priority } } }`,
		map[string]any{"id": id, "i": map[string]any{
			"kind": "chat", "strategy": "priority_overflow", "cacheAffinity": true,
			"tiers": []any{map[string]any{"entries": []any{entry("only", 2)}}},
		}})
	if !strings.Contains(out, `"name":"smart"`) || !strings.Contains(out, `"strategy":"priority_overflow"`) || strings.Contains(out, `"priority":1`) {
		t.Fatalf("update: %s", out)
	}

	delete(dir, pid)
	if out = gql(t, h, `{ virtualModels { tiers { entries { status } } } }`, nil); !strings.Contains(out, `"status":"provider_missing"`) {
		t.Fatalf("status after provider removal: %s", out)
	}

	if out = gql(t, h, `mutation($id: ID!) { deleteVirtualModel(id: $id) }`, map[string]any{"id": id}); !strings.Contains(out, `"deleteVirtualModel":true`) {
		t.Fatalf("delete: %s", out)
	}
	if out = gql(t, h, `{ virtualModels { id } }`, nil); !strings.Contains(out, `"virtualModels":[]`) {
		t.Fatalf("after delete: %s", out)
	}
}

func TestVirtualModelUserErrorsAreShown(t *testing.T) {
	h := virtualModelsHandler(t, directory{})
	out := gql(t, h, `mutation { createVirtualModel(input: {name: "bad name", kind: chat, strategy: round_robin, cacheAffinity: true, tiers: []}) { id } }`, nil)
	if !strings.Contains(out, "name must be 1-64 characters") {
		t.Fatalf("validation message hidden: %s", out)
	}
}

func TestUpdateVirtualModelInputHasNoName(t *testing.T) {
	schema := NewExecutableSchema(Config{}).Schema()
	if schema.Types["UpdateVirtualModelInput"].Fields.ForName("name") != nil {
		t.Error("names are immutable: UpdateVirtualModelInput must not have name")
	}
	if schema.Types["CreateVirtualModelInput"].Fields.ForName("name") == nil {
		t.Error("CreateVirtualModelInput.name must exist")
	}
}
