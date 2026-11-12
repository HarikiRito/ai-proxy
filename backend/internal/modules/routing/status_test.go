package routing

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
)

func TestSnapshotReportsLiveKeyState(t *testing.T) {
	named := func(id, name string, enabled bool, max int) providers.Key {
		return providers.Key{ID: id, Name: name, Enabled: enabled, MaxInFlight: max, Weight: 1}
	}
	pb := prov("pb", named("b2", "zed", true, 2), named("b1", "alpha", true, 2))
	pb.Name = "B"
	pa := prov("pa", named("a1", "busy", true, 2), named("a2", "cool", true, 1), named("a3", "old", true, 1), named("a4", "off", false, 1))
	pa.Name = "A"
	pc := prov("pc", named("c1", "idle", true, 3))
	pc.Name, pc.Enabled = "C", false
	clk := &clock{t: time.Unix(1_000_000, 0)}
	e := New(Deps{Providers: fakeProviders{"pa": pa, "pb": pb, "pc": pc}, Now: clk.now})

	e.mu.Lock()
	e.take("a1", time.Minute)
	e.take("a1", time.Minute)
	e.take("a1", time.Minute) // over the limit, free stays 0
	e.mu.Unlock()
	e.cool("a2", time.Minute)
	e.cool("a3", time.Second)
	clk.advance(2 * time.Second)

	got, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	byID := map[string]KeyStatus{}
	for _, k := range got {
		order = append(order, k.ProviderName+"/"+k.KeyName)
		byID[k.KeyID] = k
	}
	want := []string{"A/busy", "A/cool", "A/off", "A/old", "B/alpha", "B/zed", "C/idle"}
	if !slices.Equal(order, want) {
		t.Fatalf("order %v", order)
	}
	if k := byID["a1"]; k.InFlight != 3 || k.Free != 0 || k.MaxInFlight != 2 {
		t.Fatalf("busy %+v", k)
	}
	if k := byID["a2"]; k.CooldownUntil == nil || !k.CooldownUntil.Equal(clk.now().Add(58*time.Second)) {
		t.Fatalf("cooling %+v", k)
	}
	if byID["a3"].CooldownUntil != nil {
		t.Fatal("expired cooldown shown")
	}
	if k := byID["a4"]; k.KeyEnabled || k.Enabled() {
		t.Fatalf("disabled key %+v", k)
	}
	if k := byID["c1"]; k.ProviderEnabled || k.Enabled() || k.Free != 3 || k.InFlight != 0 {
		t.Fatalf("disabled provider %+v", k)
	}
	if k := byID["b1"]; !k.Enabled() || k.Free != 2 {
		t.Fatalf("idle %+v", k)
	}
}
