package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/usage"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

func usageHandler(t *testing.T) http.Handler {
	t.Helper()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	svc := usage.New(usage.Deps{DB: client})
	n := func(v int) *int { return &v }
	s := func(v string) *string { return &v }
	at := func(day, hour int) time.Time { return time.Date(2026, 6, day, hour, 0, 0, 0, time.UTC) }
	for _, r := range []usage.Record{
		{ProxyKeyID: "k1", VirtualModel: "fast", ProviderName: s("p1"), Status: 200, InputTokens: n(10), OutputTokens: n(2), CacheReadTokens: n(4), CreatedAt: at(1, 23)},
		{ProxyKeyID: "k1", VirtualModel: "fast", ProviderName: s("p2"), Status: 502, CreatedAt: at(2, 1)},
		{ProxyKeyID: "k2", VirtualModel: "slow", ProviderName: s("p1"), Status: 200, InputTokens: n(5), OutputTokens: n(1), CacheCreationTokens: n(3), CreatedAt: at(2, 2)},
	} {
		svc.Record(r)
	}
	svc.Close()
	return NewHandler(&Resolver{Usage: svc}, false)
}

func TestUsageRecordsFilterAndPaging(t *testing.T) {
	h := usageHandler(t)
	const q = `query($f: UsageFilter, $l: Int, $o: Int) { usageRecords(filter: $f, limit: $l, offset: $o) { total items { proxyKeyId virtualModel status inputTokens cacheCreationTokens } } }`
	var got struct {
		Data struct {
			UsageRecords struct {
				Total int
				Items []struct {
					ProxyKeyID, VirtualModel string
					Status                   int
					InputTokens              *int
				}
			}
		}
	}
	run := func(vars map[string]any) string {
		out := gql(t, h, q, vars)
		got.Data.UsageRecords.Items = nil
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(out)
		}
		return out
	}
	run(map[string]any{"f": map[string]any{"proxyKeyId": "k1", "virtualModel": "fast"}, "l": 1, "o": 1})
	if r := got.Data.UsageRecords; r.Total != 2 || len(r.Items) != 1 || r.Items[0].Status != 200 || *r.Items[0].InputTokens != 10 {
		t.Fatalf("%+v", r)
	}
	run(map[string]any{"f": map[string]any{"from": "2026-06-02T00:00:00Z", "to": "2026-06-02T01:30:00Z"}})
	if r := got.Data.UsageRecords; r.Total != 1 || r.Items[0].Status != 502 || r.Items[0].InputTokens != nil {
		t.Fatalf("time range: %+v", r)
	}
	for name, vars := range map[string]map[string]any{
		"limit over max":  {"l": 201},
		"negative offset": {"o": -1},
		"from after to":   {"f": map[string]any{"from": "2026-06-03T00:00:00Z", "to": "2026-06-02T00:00:00Z"}},
	} {
		if out := run(vars); !strings.Contains(out, `"errors"`) || strings.Contains(out, "internal server error") {
			t.Errorf("%s: want a user error, got %s", name, out)
		}
	}
}

func TestUsageTotalsPerGroup(t *testing.T) {
	h := usageHandler(t)
	const q = `query($g: UsageGroup!) { usageTotals(groupBy: $g) { label requests inputTokens outputTokens cacheReadTokens cacheCreationTokens errors } }`
	for group, want := range map[string]string{
		"DAY":      `[{"label":"2026-06-01","requests":1,"inputTokens":10,"outputTokens":2,"cacheReadTokens":4,"cacheCreationTokens":0,"errors":0},{"label":"2026-06-02","requests":2,"inputTokens":5,"outputTokens":1,"cacheReadTokens":0,"cacheCreationTokens":3,"errors":1}]`,
		"MODEL":    `[{"label":"fast","requests":2,"inputTokens":10,"outputTokens":2,"cacheReadTokens":4,"cacheCreationTokens":0,"errors":1},{"label":"slow","requests":1,"inputTokens":5,"outputTokens":1,"cacheReadTokens":0,"cacheCreationTokens":3,"errors":0}]`,
		"PROVIDER": `[{"label":"p1","requests":2,"inputTokens":15,"outputTokens":3,"cacheReadTokens":4,"cacheCreationTokens":3,"errors":0},{"label":"p2","requests":1,"inputTokens":0,"outputTokens":0,"cacheReadTokens":0,"cacheCreationTokens":0,"errors":1}]`,
	} {
		if out := gql(t, h, q, map[string]any{"g": group}); out != `{"data":{"usageTotals":`+want+`}}` {
			t.Errorf("%s: %s", group, out)
		}
	}
}
