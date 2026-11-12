package routing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

type fakeModels virtualmodels.Model

func (f fakeModels) ByName(context.Context, string) (virtualmodels.Model, error) {
	return virtualmodels.Model(f), nil
}

type handler func() (*http.Response, error)

// world is an engine whose upstreams are in-process handlers keyed by provider id.
type world struct {
	t      *testing.T
	e      *Engine
	clk    *clock
	hosts  map[string]string
	handle map[string]handler
	served []string
	reqs   map[string]*http.Request
	bodies map[string]string
}

func reply(status int, body string) handler {
	return func() (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}

func completion(cached int) handler {
	return reply(200, fmt.Sprintf(`{"usage":{"prompt_tokens":9,"prompt_tokens_details":{"cached_tokens":%d}}}`, cached))
}

func claudeMessage(cacheRead int) handler {
	return reply(200, fmt.Sprintf(`{"id":"m","model":"c","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":3,"cache_read_input_tokens":%d}}`, cacheRead))
}

func stream(chunks ...string) handler {
	return func() (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(strings.Join(chunks, "")))}, nil
	}
}

func (w *world) RoundTrip(r *http.Request) (*http.Response, error) {
	id := w.hosts[r.URL.Hostname()]
	b, _ := io.ReadAll(r.Body)
	w.served, w.reqs[id], w.bodies[id] = append(w.served, strings.TrimPrefix(id, "p")), r, string(b)
	if h := w.handle[id]; h != nil {
		return h()
	}
	return completion(0)()
}

func affProv(id string, keys ...providers.Key) providers.Provider {
	p := prov(id, keys...)
	p.CacheAffinitySeconds = 600
	return p
}

func newWorld(t *testing.T, m virtualmodels.Model, provs ...providers.Provider) *world {
	t.Helper()
	w := &world{t: t, clk: &clock{t: time.Unix(1_000_000, 0)}, hosts: map[string]string{}, handle: map[string]handler{}, reqs: map[string]*http.Request{}, bodies: map[string]string{}}
	fp := fakeProviders{}
	for _, p := range provs {
		u, _ := url.Parse(p.BaseURL)
		fp[p.ID], w.hosts[u.Hostname()] = p, p.ID
	}
	m.CacheAffinity = true
	w.e = New(Deps{Models: fakeModels(m), Providers: fp, Settings: fakeSettings(testCfg), Now: w.clk.now, Client: &http.Client{Transport: w}})
	return w
}

func twoRoundRobin(t *testing.T) *world {
	m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)})
	return newWorld(t, m, affProv("pa", key("ka", 9, 1)), affProv("pb", key("kb", 9, 1)))
}

func conv(extra string) string {
	return `{"model":"m",` + extra + `"messages":[{"role":"user","content":"hi"}]}`
}

const explicit = `"session_id":"s1",`

// ask sends one chat request as proxy key pk and reads a stream to its end.
func (w *world) ask(pk, body string, hdr ...string) (Result, error) {
	w.t.Helper()
	var in map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		w.t.Fatal(err)
	}
	h := http.Header{}
	for i := 0; i+1 < len(hdr); i += 2 {
		h.Set(hdr[i], hdr[i+1])
	}
	ctx := WithAffinity(context.Background(), pk)
	call := w.e.Do
	if string(in["stream"]) == "true" {
		call = w.e.Stream
	}
	res, err := call(ctx, "m", h, in)
	if err == nil && res.Stream != nil {
		_, _ = io.Copy(io.Discard, res.Stream)
		_ = res.Stream.Close()
	}
	return res, err
}

func (w *world) must(body string, hdr ...string) Result {
	w.t.Helper()
	res, err := w.ask("pk", body, hdr...)
	if err != nil {
		w.t.Fatal(err)
	}
	return res
}

func (w *world) pins() int {
	w.e.pins.mu.Lock()
	defer w.e.pins.mu.Unlock()
	return len(w.e.pins.m)
}

func want(t *testing.T, w *world, served string) {
	t.Helper()
	if got := strings.Join(w.served, ""); got != served {
		t.Fatalf("served %q, want %q", got, served)
	}
}

func derive(scope, model, body string, hdr ...string) *affinity {
	var b map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		panic(err)
	}
	h := http.Header{}
	for i := 0; i+1 < len(hdr); i += 2 {
		h.Set(hdr[i], hdr[i+1])
	}
	return New(Deps{}).newAffinity(WithAffinity(context.Background(), scope), model, h, b)
}

func keyOf(a *affinity) string {
	if a == nil {
		return ""
	}
	return a.key
}

func TestExplicitIDPrecedence(t *testing.T) {
	all := `"session_id":"A","prompt_cache_key":"C",`
	for name, tc := range map[string]struct{ body, hdr, id string }{
		"body session_id first": {conv(all), "B", "A"},
		"header over cache key": {conv(`"prompt_cache_key":"C",`), "B", "B"},
		"cache key last":        {conv(`"prompt_cache_key":"C",`), "", "C"},
	} {
		a := derive("pk", "m", tc.body, "X-Session-Id", tc.hdr)
		if keyOf(a) != "pk|m|"+tc.id || !a.explicit {
			t.Errorf("%s: key %q", name, keyOf(a))
		}
	}
	if a := derive("pk", "m", conv(`"session_id":7,`)); a == nil || a.explicit {
		t.Errorf("non-string session_id must fall back to the hash: %+v", a)
	}
	long := derive("pk", "m", conv(`"session_id":"`+strings.Repeat("x", 300)+`",`))
	if got := strings.TrimPrefix(long.key, "pk|m|"); len(got) != maxExplicitID {
		t.Errorf("explicit id not truncated: %d", len(got))
	}
}

func TestHashKey(t *testing.T) {
	turn1 := `{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"q1"}]}`
	turn3 := `{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"q1"},{"role":"assistant","content":"a1"},{"role":"user","content":"q2"}]}`
	base := keyOf(derive("pk", "m", turn1))
	if base == "" || keyOf(derive("pk", "m", turn3)) != base {
		t.Error("hash must be stable across turns")
	}
	for _, body := range []string{
		strings.Replace(turn1, `"sys"`, `"other"`, 1),
		strings.Replace(turn1, `"q1"`, `"q9"`, 1),
		`{"messages":[{"role":"assistant","content":"q1"}]}`,
	} {
		if keyOf(derive("pk", "m", body)) == base {
			t.Errorf("hash ignored a change in %s", body)
		}
	}
	if keyOf(derive("pk2", "m", turn1)) == base || keyOf(derive("pk", "m2", turn1)) == base {
		t.Error("key must be scoped by proxy key and model")
	}
	if derive("pk", "m", `{"messages":[{"role":"system","content":"only"}]}`) != nil || derive("", "m", turn1) != nil {
		t.Error("no opening message or no proxy key must mean no affinity")
	}
}

func TestExplicitConversationPinsAfterFirstSuccess(t *testing.T) {
	w := twoRoundRobin(t)
	first := w.must(conv(explicit))
	second := w.must(conv(explicit))
	want(t, w, "aa")
	if first.Affinity != "new" || second.Affinity != "hit" {
		t.Fatalf("labels %q %q", first.Affinity, second.Affinity)
	}
	if res, err := w.ask("other-key", conv(explicit)); err != nil || res.Affinity != "new" {
		t.Fatalf("another proxy key shares the conversation: %+v %v", res, err)
	}
	want(t, w, "aab")
}

func TestHashConversationPinsOnlyWhenCacheShown(t *testing.T) {
	cases := map[string]struct {
		handler handler
		body    string
		pinned  bool
	}{
		"openai cached":        {completion(4), conv(""), true},
		"openai not cached":    {completion(0), conv(""), false},
		"claude cache read":    {claudeMessage(4), conv(""), true},
		"claude no cache read": {claudeMessage(0), conv(""), false},
		"openai stream usage": {
			stream(`data: {"choices":[],"usage":{"prompt_tokens":9,"prompt_tokens_details":{"cached_tokens":4}}}`+"\n\n", "data: [DONE]\n\n"),
			`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`, true,
		},
		"openai stream without usage": {stream("data: {\"choices\":[]}\n\n", "data: [DONE]\n\n"), `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`, false},
		"claude stream cache read": {
			stream(`data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":3,"cache_read_input_tokens":4}}}`+"\n\n", "data: {\"type\":\"message_stop\"}\n\n"),
			`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`, true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := twoRoundRobin(t)
			w.handle["pa"] = tc.handler
			if strings.HasPrefix(name, "claude") {
				m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)})
				pa := affProv("pa", key("ka", 9, 1))
				pa.Kind = providers.KindAnthropic
				w = newWorld(t, m, pa, affProv("pb", key("kb", 9, 1)))
				w.handle["pa"] = tc.handler
			}
			w.must(tc.body)
			w.must(tc.body)
			if got := strings.Join(w.served, ""); (got == "aa") != tc.pinned || (tc.pinned && w.pins() != 1) || (!tc.pinned && w.pins() != 0) {
				t.Fatalf("served %q pins %d, want pinned=%v", got, w.pins(), tc.pinned)
			}
		})
	}
}

func TestBusyPinnedKeyFallsBackWithoutWaiting(t *testing.T) {
	m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)})
	w := newWorld(t, m, affProv("pa", key("k1", 1, 1), key("k2", 1, 1)), affProv("pb", key("kb", 9, 1)))
	w.must(conv(explicit))
	hold := func(keyID string) {
		w.e.mu.Lock()
		w.e.take(keyID, time.Hour)
		w.e.mu.Unlock()
	}
	hold("k1")
	start := time.Now()
	w.must(conv(explicit))
	hold("k2")
	res := w.must(conv(explicit))
	if want(t, w, "aab"); res.Affinity != "break" || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("label %q after %v", res.Affinity, time.Since(start))
	}
	if w.reqs["pa"].Header.Get("Authorization") != "Bearer secret-k2" {
		t.Fatalf("second request did not use the sibling key: %v", w.reqs["pa"].Header)
	}
}

func TestFailureOnPinnedTargetBreaksPin(t *testing.T) {
	failures := map[string]handler{
		"429":     reply(429, `{}`),
		"500":     reply(502, `{}`),
		"network": func() (*http.Response, error) { return nil, errors.New("reset") },
	}
	for name, h := range failures {
		t.Run(name, func(t *testing.T) {
			w := twoRoundRobin(t)
			w.must(conv(explicit))
			w.handle["pa"] = h
			broken := w.must(conv(explicit))
			if broken.Affinity != "break" {
				t.Fatalf("label %q", broken.Affinity)
			}
			w.handle["pa"] = nil
			after := w.must(conv(explicit))
			if want(t, w, "aabb"); after.Affinity != "hit" {
				t.Fatalf("fallback not re-pinned: %q", after.Affinity)
			}
		})
	}
	t.Run("hash pin is dropped and not replaced without cache", func(t *testing.T) {
		w := twoRoundRobin(t)
		w.handle["pa"] = completion(3)
		w.must(conv(""))
		w.handle["pa"] = reply(502, `{}`)
		w.must(conv(""))
		if w.pins() != 0 {
			t.Fatal("pin survived a failure on its target")
		}
	})
}

func TestPassthroughClientErrorsNeitherUpdateNorDrop(t *testing.T) {
	w := twoRoundRobin(t)
	w.must(conv(explicit))
	w.handle["pa"] = reply(400, `{"error":"bad"}`)
	if res := w.must(conv(explicit)); res.Status != 400 || res.Affinity != "hit" {
		t.Fatalf("%+v", res)
	}
	w.handle["pa"] = nil
	if res := w.must(conv(explicit)); res.Affinity != "hit" {
		t.Fatalf("pin lost after a 400: %q", res.Affinity)
	}
	fresh := twoRoundRobin(t)
	fresh.handle["pa"], fresh.handle["pb"] = reply(400, `{}`), reply(400, `{}`)
	fresh.must(conv(explicit))
	if fresh.pins() != 0 {
		t.Fatal("an error reply created a pin")
	}
}

func TestPinLifetime(t *testing.T) {
	t.Run("expires when idle and refreshes on use", func(t *testing.T) {
		w := twoRoundRobin(t)
		w.must(conv(explicit))
		w.clk.advance(500 * time.Second)
		w.must(conv(explicit))
		w.clk.advance(500 * time.Second)
		if res := w.must(conv(explicit)); res.Affinity != "hit" {
			t.Fatalf("use did not refresh the pin: %q", res.Affinity)
		}
		w.clk.advance(601 * time.Second)
		if res := w.must(conv(explicit)); res.Affinity != "new" {
			t.Fatalf("idle pin still live: %q", res.Affinity)
		}
	})
	t.Run("client cache_control 1h extends it", func(t *testing.T) {
		w := twoRoundRobin(t)
		body := conv(explicit + `"cache_control":{"type":"ephemeral","ttl":"1h"},`)
		w.must(body)
		w.clk.advance(3000 * time.Second)
		if res := w.must(body); res.Affinity != "hit" {
			t.Fatalf("%q", res.Affinity)
		}
	})
	t.Run("zero seconds never pins", func(t *testing.T) {
		m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)})
		w := newWorld(t, m, prov("pa", key("ka", 9, 1)), prov("pb", key("kb", 9, 1)))
		w.must(conv(explicit))
		w.must(conv(explicit))
		if want(t, w, "ab"); w.pins() != 0 {
			t.Fatal("pinned a provider with 0 seconds")
		}
	})
}

func TestAffinityOffSpreadsAndHintsNothing(t *testing.T) {
	w := twoRoundRobin(t)
	w.e.models = fakeModels(virtualmodels.Model{
		Name: "m", Kind: virtualmodels.KindChat, Strategy: virtualmodels.StrategyRoundRobin, CacheAffinity: false,
		Tiers: []virtualmodels.Tier{{Entries: []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)}}},
	})
	w.must(conv(explicit))
	res := w.must(conv(explicit))
	if want(t, w, "ab"); res.Affinity != "" || w.pins() != 0 {
		t.Fatalf("label %q pins %d", res.Affinity, w.pins())
	}
}

func TestWithoutProxyKeyContextNoAffinity(t *testing.T) {
	w := twoRoundRobin(t)
	var in map[string]json.RawMessage
	_ = json.Unmarshal([]byte(conv(explicit)), &in)
	for range 2 {
		if res, err := w.e.Do(context.Background(), "m", http.Header{}, in); err != nil || res.Affinity != "" {
			t.Fatalf("%+v %v", res, err)
		}
	}
	want(t, w, "ab")
}

func hostWorld(t *testing.T, baseURL string, kind providers.Kind) *world {
	t.Helper()
	m := model(virtualmodels.StrategyPriorityOverflow, []virtualmodels.Entry{ent("a", "pa", 1)})
	p := affProv("pa", key("ka", 9, 1))
	p.BaseURL, p.Kind = baseURL, kind
	return newWorld(t, m, p)
}

// sentFields lists the cache related fields the upstream received; absent ones are omitted.
func sentFields(w *world) map[string]string {
	var sent map[string]json.RawMessage
	_ = json.Unmarshal([]byte(w.bodies["pa"]), &sent)
	out := map[string]string{}
	for _, k := range []string{"prompt_cache_key", "session_id"} {
		var v string
		if json.Unmarshal(sent[k], &v) == nil && v != "" {
			out[k] = v
		}
	}
	for _, k := range []string{"X-Session-Id", "X-Session-Affinity"} {
		if v := w.reqs["pa"].Header.Get(k); v != "" {
			out[k] = v
		}
	}
	return out
}

func TestHintInjection(t *testing.T) {
	sum := sha256.Sum256([]byte(keyOf(derive("pk", "m", conv("")))))
	hash := hex.EncodeToString(sum[:])
	for base, field := range map[string]string{
		"https://api.openai.com/v1":             "prompt_cache_key",
		"https://api.fireworks.ai/inference/v1": "X-Session-Affinity",
		"https://openrouter.ai/api/v1":          "session_id",
		"https://api.openai.com.evil.test/v1":   "",
		"https://my-llm.internal/v1":            "",
	} {
		w := hostWorld(t, base, providers.KindOpenAICompatible)
		w.must(conv(""))
		wantFields := map[string]string{}
		if field != "" {
			wantFields[field] = hash
		}
		if got := sentFields(w); !reflect.DeepEqual(got, wantFields) {
			t.Errorf("%s: got %v want %v", base, got, wantFields)
		}
	}
}

func TestHintNeverTouchesClientValues(t *testing.T) {
	for name, tc := range map[string]struct {
		base, body string
		hdr        []string
		want       map[string]string
	}{
		"body prompt_cache_key": {"https://api.openai.com/v1", conv(`"prompt_cache_key":"mine",`), nil, map[string]string{"prompt_cache_key": "mine"}},
		"body session_id":       {"https://openrouter.ai/api/v1", conv(`"session_id":"mine",`), nil, map[string]string{"session_id": "mine"}},
		"x-session-id header":   {"https://openrouter.ai/api/v1", conv(""), []string{"X-Session-Id", "mine"}, map[string]string{"X-Session-Id": "mine"}},
		"own affinity header":   {"https://api.fireworks.ai/v1", conv(""), []string{"X-Session-Affinity", "mine"}, map[string]string{"X-Session-Affinity": "mine"}},
	} {
		w := hostWorld(t, tc.base, providers.KindOpenAICompatible)
		w.must(tc.body, tc.hdr...)
		if got := sentFields(w); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v want %v", name, got, tc.want)
		}
	}
	t.Run("claude targets get no hint", func(t *testing.T) {
		w := hostWorld(t, "https://api.openai.com/v1", providers.KindAnthropic)
		w.handle["pa"] = claudeMessage(0)
		w.must(conv(""))
		if got := sentFields(w); len(got) != 0 || strings.Contains(w.bodies["pa"], "cache_key") {
			t.Fatalf("%v %s", got, w.bodies["pa"])
		}
	})
	t.Run("body otherwise byte for byte", func(t *testing.T) {
		w := hostWorld(t, "https://api.openai.com/v1", providers.KindOpenAICompatible)
		w.must(`{ "model":"m", "temperature": 0.5 , "messages":[{"role":"user","content":"a <b>"}]}`)
		if !strings.Contains(w.bodies["pa"], `"temperature":0.5`) || !strings.Contains(w.bodies["pa"], `"messages":[{"role":"user","content":"a <b>"}]`) {
			t.Fatalf("%s", w.bodies["pa"])
		}
	})
}

func TestNoHintWhenAffinityOff(t *testing.T) {
	w := hostWorld(t, "https://api.openai.com/v1", providers.KindOpenAICompatible)
	w.e.models = fakeModels(virtualmodels.Model{Name: "m", Kind: virtualmodels.KindChat, Tiers: []virtualmodels.Tier{{Entries: []virtualmodels.Entry{ent("a", "pa", 1)}}}})
	w.must(conv(""))
	if strings.Contains(w.bodies["pa"], "prompt_cache_key") {
		t.Fatalf("%s", w.bodies["pa"])
	}
}

func TestOtherEndpointsAreNeverPinned(t *testing.T) {
	w := twoRoundRobin(t)
	for range 2 {
		if _, err := w.e.Messages(WithAffinity(context.Background(), "pk"), "m", http.Header{}, map[string]json.RawMessage{"session_id": json.RawMessage(`"s"`)}); err == nil {
			t.Fatal("openai-only targets cannot serve native messages")
		}
	}
	if w.pins() != 0 {
		t.Fatal("pins created outside chat")
	}
}

func TestCacheTeeKeepsBytesAndFiresOnce(t *testing.T) {
	line := `data: {"usage":{"prompt_tokens_details":{"cached_tokens":5}}}` + "\n\n"
	input := "data: {\"a\":1}\n\n" + line + line + "data: [DONE]\n\n"
	fired := 0
	tee := watchCache(io.NopCloser(oneByte{strings.NewReader(input)}), func() { fired++ })
	got, err := io.ReadAll(tee)
	if err != nil || string(got) != input || fired != 1 {
		t.Fatalf("err=%v fired=%d identical=%v", err, fired, string(got) == input)
	}
}

type oneByte struct{ r io.Reader }

func (o oneByte) Read(p []byte) (int, error) { return o.r.Read(p[:min(1, len(p))]) }

func TestPinStore(t *testing.T) {
	now := time.Unix(1000, 0)
	s := newPinStore()
	s.set("live", pin{entryID: "a", keyID: "k", expires: now.Add(time.Minute)})
	s.set("old", pin{entryID: "a", keyID: "k", expires: now.Add(-time.Second)})
	s.evict(now)
	if len(s.m) != 1 {
		t.Fatalf("expired pin not evicted: %d", len(s.m))
	}
	s.drop("live", pin{entryID: "other", keyID: "k"})
	if _, ok := s.get("live", now); !ok {
		t.Fatal("drop removed a pin that names another target")
	}
	if _, ok := s.get("live", now.Add(time.Hour)); ok || len(s.m) != 0 {
		t.Fatal("get must expire lazily")
	}
}

func TestPinStoreCapEvictsSoonestExpiring(t *testing.T) {
	now := time.Unix(1000, 0)
	s := newPinStore()
	for i := range maxPins {
		s.set(fmt.Sprint("k", i), pin{expires: now.Add(time.Duration(i+10) * time.Second)})
	}
	s.set("new", pin{expires: now.Add(time.Hour)})
	if _, gone := s.m["k0"]; len(s.m) != maxPins || gone {
		t.Fatalf("size %d, soonest evicted=%v", len(s.m), !gone)
	}
	s.set("new", pin{expires: now.Add(2 * time.Hour)})
	if len(s.m) != maxPins {
		t.Fatal("refreshing an existing pin must not evict")
	}
}

func TestPinStoreConcurrent(t *testing.T) {
	s, now := newPinStore(), time.Unix(1000, 0)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				k := fmt.Sprint(i % 20)
				p := pin{entryID: "e", keyID: fmt.Sprint(g), expires: now.Add(time.Minute)}
				s.set(k, p)
				s.get(k, now)
				s.drop(k, p)
				s.evict(now)
			}
		}()
	}
	wg.Wait()
}

func TestWatchEvictsExpiredPins(t *testing.T) {
	w := twoRoundRobin(t)
	w.must(conv(explicit))
	w.clk.advance(time.Hour)
	for range pinSweepTicks {
		w.e.sweep()
	}
	w.e.pins.evict(w.clk.now())
	if w.pins() != 0 {
		t.Fatal("expired pin kept")
	}
}
