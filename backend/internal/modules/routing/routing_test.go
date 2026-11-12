package routing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/settings"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

type fakeProviders map[string]providers.Provider

func (f fakeProviders) ByIDs(_ context.Context, ids []string) (map[string]providers.Provider, error) {
	out := map[string]providers.Provider{}
	for _, id := range ids {
		if p, ok := f[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

func (f fakeProviders) Reveal(_ context.Context, keyID string) (string, error) {
	return "secret-" + keyID, nil
}

type fakeSettings settings.Settings

func (f fakeSettings) Get(context.Context) (settings.Settings, error) {
	return settings.Settings(f), nil
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var testCfg = settings.Settings{MaxBodyBytes: 1 << 20, RequestTimeoutSeconds: 30, QueueWaitSeconds: 1, DefaultCooldownSeconds: 60}

func key(id string, maxInFlight, weight int) providers.Key {
	return providers.Key{ID: id, Enabled: true, MaxInFlight: maxInFlight, Weight: weight}
}

func prov(id string, keys ...providers.Key) providers.Provider {
	return providers.Provider{ID: id, Name: id, Kind: providers.KindOpenAICompatible, Enabled: true, BaseURL: "http://" + id + ".test", Keys: keys}
}

func ent(id, providerID string, weight int) virtualmodels.Entry {
	return virtualmodels.Entry{ID: id, ProviderID: providerID, Model: id, Weight: weight, Status: virtualmodels.StatusOK}
}

func model(s virtualmodels.Strategy, tiers ...[]virtualmodels.Entry) virtualmodels.Model {
	m := virtualmodels.Model{Name: "m", Kind: virtualmodels.KindChat, Strategy: s}
	for i, entries := range tiers {
		m.Tiers = append(m.Tiers, virtualmodels.Tier{Priority: i, Entries: entries})
	}
	return m
}

func setup(t *testing.T, m virtualmodels.Model, provs ...providers.Provider) (*Engine, *request, *clock) {
	t.Helper()
	fp := fakeProviders{}
	for _, p := range provs {
		fp[p.ID] = p
	}
	clk := &clock{t: time.Unix(1_000_000, 0)}
	e := New(Deps{Providers: fp, Settings: fakeSettings(testCfg), Now: clk.now})
	chain, err := e.chain(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	return e, &request{e: e, ctx: context.Background(), cfg: testCfg, model: m, chain: chain, ttl: time.Minute, tried: map[string]bool{}}, clk
}

// drain picks n times, optionally releasing each lease at once, and returns the chosen entry ids.
func drain(r *request, n int, release bool) []string {
	var got []string
	for range n {
		a, _, ok := r.pick()
		if !ok {
			break
		}
		got = append(got, a.t.entry.ID)
		if release {
			r.e.release(a.lease)
		}
	}
	return got
}

func count(ids []string) map[string]int {
	out := map[string]int{}
	for _, id := range ids {
		out[id]++
	}
	return out
}

func TestRoundRobinSpreadsEvenly(t *testing.T) {
	m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1), ent("c", "pc", 1)})
	_, r, _ := setup(t, m, prov("pa", key("ka", 9, 1)), prov("pb", key("kb", 9, 1)), prov("pc", key("kc", 9, 1)))
	if got := count(drain(r, 9, true)); got["a"] != 3 || got["b"] != 3 || got["c"] != 3 {
		t.Fatalf("spread %v", got)
	}
}

func TestWeightedRoundRobinRatio(t *testing.T) {
	m := model(virtualmodels.StrategyWeightedRoundRobin, []virtualmodels.Entry{ent("a", "pa", 5), ent("b", "pb", 1)})
	_, r, _ := setup(t, m, prov("pa", key("ka", 9, 1)), prov("pb", key("kb", 9, 1)))
	if got := count(drain(r, 600, true)); got["a"] != 500 || got["b"] != 100 {
		t.Fatalf("ratio %v", got)
	}
}

func TestPriorityOverflowFillsFirstThenSpills(t *testing.T) {
	m := model(virtualmodels.StrategyPriorityOverflow, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)})
	_, r, _ := setup(t, m, prov("pa", key("ka", 2, 1)), prov("pb", key("kb", 1, 1)))
	got := drain(r, 5, false)
	if fmt.Sprint(got) != "[a a b]" {
		t.Fatalf("got %v", got)
	}
}

func TestLeastInFlightPicksMostFree(t *testing.T) {
	m := model(virtualmodels.StrategyLeastInFlight, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)})
	_, r, _ := setup(t, m, prov("pa", key("ka", 2, 1)), prov("pb", key("kb", 5, 1)))
	got := drain(r, 9, false)
	if got[0] != "b" || got[1] != "b" || got[2] != "b" || fmt.Sprint(count(got)) != "map[a:2 b:5]" {
		t.Fatalf("got %v", got)
	}
}

func TestLowerTierUsedOnlyWhenHigherHasNoRoom(t *testing.T) {
	m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1)}, []virtualmodels.Entry{ent("b", "pb", 1)})
	e, r, _ := setup(t, m, prov("pa", key("ka", 1, 1)), prov("pb", key("kb", 9, 1)))
	first, _, _ := r.pick()
	second, _, _ := r.pick()
	if first.t.entry.ID != "a" || second.t.entry.ID != "b" {
		t.Fatalf("got %s then %s", first.t.entry.ID, second.t.entry.ID)
	}
	e.release(first.lease)
	if third, _, _ := r.pick(); third.t.entry.ID != "a" {
		t.Fatalf("tier 0 not preferred again: %s", third.t.entry.ID)
	}
}

func TestKeyChoiceByWeightSkipsDisabledAndCooling(t *testing.T) {
	disabled := key("kd", 9, 100)
	disabled.Enabled = false
	m := model(virtualmodels.StrategyPriorityOverflow, []virtualmodels.Entry{ent("a", "pa", 1)})
	e, r, _ := setup(t, m, prov("pa", key("k3", 99, 3), key("k1", 99, 1), key("kc", 99, 50), disabled))
	e.cool("kc", time.Minute)
	keys := map[string]int{}
	for range 400 {
		a, _, ok := r.pick()
		if !ok {
			t.Fatal("no pick")
		}
		keys[a.key.ID]++
		e.release(a.lease)
	}
	if keys["k3"] != 300 || keys["k1"] != 100 || len(keys) != 2 {
		t.Fatalf("keys %v", keys)
	}
}

func TestCooldownExpires(t *testing.T) {
	m := model(virtualmodels.StrategyPriorityOverflow, []virtualmodels.Entry{ent("a", "pa", 1)})
	e, r, clk := setup(t, m, prov("pa", key("ka", 1, 1)))
	e.cool("ka", 10*time.Second)
	if _, out, ok := r.pick(); ok || !out.untried || out.soonest != 10*time.Second {
		t.Fatalf("cooling key picked or no hint: ok=%v out=%+v", ok, out)
	}
	clk.advance(11 * time.Second)
	if _, _, ok := r.pick(); !ok {
		t.Fatal("key still cooling after expiry")
	}
}

func TestLeaseLimitReleaseAndWatchdog(t *testing.T) {
	m := model(virtualmodels.StrategyPriorityOverflow, []virtualmodels.Entry{ent("a", "pa", 1)})
	e, r, clk := setup(t, m, prov("pa", key("ka", 2, 1)))
	l1, _, _ := r.pick()
	r.pick()
	if _, _, ok := r.pick(); ok {
		t.Fatal("third lease granted over limit 2")
	}
	e.release(l1.lease)
	e.release(l1.lease) // idempotent
	if e.inflight["ka"] != 1 {
		t.Fatalf("inflight %d", e.inflight["ka"])
	}
	if _, _, ok := r.pick(); !ok {
		t.Fatal("release did not free capacity")
	}
	clk.advance(2 * time.Minute)
	e.sweep()
	if len(e.leases) != 0 || len(e.inflight) != 0 {
		t.Fatalf("watchdog left leases=%d inflight=%v", len(e.leases), e.inflight)
	}
}

func raise(peak *atomic.Int32, n int32) {
	for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
	}
}

func TestConcurrentPicksNeverExceedLimit(t *testing.T) {
	const limit = 3
	m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1)})
	e, r, _ := setup(t, m, prov("pa", key("ka", limit, 1)))
	var cur, peak atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				a, _, ok := r.pick()
				if !ok {
					continue
				}
				n := cur.Add(1)
				raise(&peak, n)
				cur.Add(-1)
				e.release(a.lease)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > limit || len(e.inflight) != 0 {
		t.Fatalf("peak %d inflight %v", peak.Load(), e.inflight)
	}
}

func TestWatchStopsWithContext(t *testing.T) {
	e := New(Deps{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Watch(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not stop")
	}
}

func TestUnusableTargetsAreSkipped(t *testing.T) {
	anthropic := prov("pn", key("kn", 1, 1))
	anthropic.Kind = providers.KindAnthropic
	off := prov("po", key("ko", 1, 1))
	off.Enabled = false
	bad := ent("bad", "pb", 1)
	bad.Status = virtualmodels.StatusProviderDisabled
	m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("n", "pn", 1), ent("o", "po", 1), bad, ent("gone", "none", 1)})
	_, r, _ := setup(t, m, anthropic, off, prov("pb", key("kb", 1, 1)))
	if len(r.chain) != 0 {
		t.Fatalf("chain %+v", r.chain)
	}
	var re *Error
	if err := r.exhausted(); !errors.As(err, &re) || re.Status != http.StatusServiceUnavailable {
		t.Fatalf("got %v", err)
	}
}

func TestCooldownHeader(t *testing.T) {
	clk := &clock{t: time.Unix(1_000_000, 0)}
	r := &request{e: &Engine{now: clk.now}, cfg: testCfg}
	date := clk.now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	for header, want := range map[string]time.Duration{
		"7": 7 * time.Second, date: 90 * time.Second, "": 60 * time.Second, "junk": 60 * time.Second, "0": 60 * time.Second,
		"999999999": maxCooldown,
	} {
		if got := r.cooldownFor(header); got != want {
			t.Errorf("Retry-After %q: got %v want %v", header, got, want)
		}
	}
}
