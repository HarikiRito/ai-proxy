package routing

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

func nconv(extra string) string {
	return `{"model":"m",` + extra + `"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
}

const (
	nstream = `"stream":true,`
	session = "X-Session-Id"
)

func nativeWorld(t *testing.T) *world {
	m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)})
	pa, pb := affProv("pa", key("ka", 9, 1)), affProv("pb", key("kb", 9, 1))
	pa.Kind, pb.Kind = providers.KindAnthropic, providers.KindAnthropic
	w := newWorld(t, m, pa, pb)
	w.native = true
	w.handle["pa"], w.handle["pb"] = claudeMessage(0), claudeMessage(0)
	return w
}

func nderive(scope, model, body string, hdr ...string) *affinity {
	var b map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		panic(err)
	}
	h := http.Header{}
	for i := 0; i+1 < len(hdr); i += 2 {
		h.Set(hdr[i], hdr[i+1])
	}
	return New(Deps{}).newAffinity(WithAffinity(context.Background(), scope), model, h, messagesPayload(b))
}

func TestMessagesConversationKey(t *testing.T) {
	turn1 := `{"system":"sys","messages":[{"role":"user","content":"q1"}]}`
	turn2 := `{"system":"sys","messages":[{"role":"user","content":"q1"},{"role":"assistant","content":"a1"},{"role":"user","content":"q2"}]}`
	base := keyOf(nderive("pk", "m", turn1))
	if base == "" || keyOf(nderive("pk", "m", turn2)) != base {
		t.Error("hash must be stable across turns")
	}
	for name, body := range map[string]string{
		"system text":    strings.Replace(turn1, `"sys"`, `"other"`, 1),
		"first message":  strings.Replace(turn1, `"q1"`, `"q9"`, 1),
		"system blocks":  strings.Replace(turn1, `"sys"`, `[{"type":"text","text":"sys"}]`, 1),
		"no system":      `{"messages":[{"role":"user","content":"q1"}]}`,
		"content blocks": strings.Replace(turn1, `"q1"`, `[{"type":"text","text":"q1"}]`, 1),
	} {
		if keyOf(nderive("pk", "m", body)) == base {
			t.Errorf("hash ignored a change in %s", name)
		}
	}
	if keyOf(nderive("pk2", "m", turn1)) == base || keyOf(nderive("pk", "m2", turn1)) == base {
		t.Error("key must be scoped by proxy key and model")
	}
	if nderive("pk", "m", `{"system":"s","messages":[]}`) != nil || nderive("pk", "m", `{"system":"s"}`) != nil {
		t.Error("no messages must mean no affinity")
	}
	a := nderive("pk", "m", `{"session_id":"body","messages":[{"role":"user","content":"q1"}]}`, session, "hdr")
	if keyOf(a) != "pk|m|hdr" || !a.explicit {
		t.Errorf("header must win: %q", keyOf(a))
	}
	if a = nderive("pk", "m", `{"session_id":"body","prompt_cache_key":"c","messages":[{"role":"user","content":"q1"}]}`); a.explicit {
		t.Error("body fields are not conversation ids on native bodies")
	}
	long := nderive("pk", "m", turn1, session, strings.Repeat("x", 300))
	if got := strings.TrimPrefix(long.key, "pk|m|"); len(got) != maxExplicitID {
		t.Errorf("explicit id not truncated: %d", len(got))
	}
}

func TestMessagesExplicitHeaderPins(t *testing.T) {
	w := nativeWorld(t)
	first := w.must(nconv(""), session, "s1")
	second := w.must(nconv(""), session, "s1")
	want(t, w, "aa")
	if first.Affinity != "new" || second.Affinity != "hit" {
		t.Fatalf("labels %q %q", first.Affinity, second.Affinity)
	}
	if res, err := w.ask("other-key", nconv(""), session, "s1"); err != nil || res.Affinity != "new" {
		t.Fatalf("another proxy key shares the conversation: %+v %v", res, err)
	}
	want(t, w, "aab")
}

func TestMessagesHashPinsOnlyWhenCacheShown(t *testing.T) {
	startEvent := func(read string) string {
		return `event: message_start` + "\n" + `data: {"type":"message_start","message":{"usage":{"input_tokens":3,"cache_read_input_tokens":` + read + `}}}` + "\n\n"
	}
	cases := map[string]struct {
		handler handler
		body    string
		pinned  bool
	}{
		"buffered cache read":    {claudeMessage(4), nconv(""), true},
		"buffered no cache read": {claudeMessage(0), nconv(""), false},
		"stream message_start":   {stream(startEvent("4"), "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"), nconv(nstream), true},
		"stream no cache read":   {stream(startEvent("0"), "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"), nconv(nstream), false},
		"stream message_delta": {
			stream(startEvent("0"), `data: {"type":"message_delta","usage":{"output_tokens":2,"cache_read_input_tokens":4}}`+"\n\n"),
			nconv(nstream), true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := nativeWorld(t)
			w.handle["pa"] = tc.handler
			w.must(tc.body)
			w.must(tc.body)
			if got := strings.Join(w.served, ""); (got == "aa") != tc.pinned || (tc.pinned && w.pins() != 1) || (!tc.pinned && w.pins() != 0) {
				t.Fatalf("served %q pins %d, want pinned=%v", got, w.pins(), tc.pinned)
			}
		})
	}
}

func TestMessagesBusyPinnedKeyFallsBackWithoutWaiting(t *testing.T) {
	m := model(virtualmodels.StrategyRoundRobin, []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)})
	pa, pb := affProv("pa", key("k1", 1, 1), key("k2", 1, 1)), affProv("pb", key("kb", 9, 1))
	pa.Kind, pb.Kind = providers.KindAnthropic, providers.KindAnthropic
	w := newWorld(t, m, pa, pb)
	w.native = true
	w.must(nconv(""), session, "s1")
	w.e.mu.Lock()
	w.e.take("k1", time.Hour)
	w.e.mu.Unlock()
	start := time.Now()
	res := w.must(nconv(""), session, "s1")
	if want(t, w, "aa"); res.Affinity != "break" || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("label %q after %v", res.Affinity, time.Since(start))
	}
	if w.reqs["pa"].Header.Get("X-Api-Key") != "secret-k2" {
		t.Fatalf("did not use the sibling key: %v", w.reqs["pa"].Header)
	}
}

func TestMessagesFailureBreaksPinAndRepins(t *testing.T) {
	for name, h := range map[string]handler{
		"429": reply(429, `{}`), "502": reply(502, `{}`),
		"network": func() (*http.Response, error) { return nil, io.ErrUnexpectedEOF },
	} {
		t.Run(name, func(t *testing.T) {
			w := nativeWorld(t)
			w.must(nconv(""), session, "s1")
			w.handle["pa"] = h
			if res := w.must(nconv(""), session, "s1"); res.Affinity != "break" {
				t.Fatalf("label %q", res.Affinity)
			}
			w.handle["pa"] = claudeMessage(0)
			res := w.must(nconv(""), session, "s1")
			if want(t, w, "aabb"); res.Affinity != "hit" {
				t.Fatalf("fallback not re-pinned: %q", res.Affinity)
			}
		})
	}
}

func TestMessagesPassthroughClientErrorNeitherUpdatesNorDrops(t *testing.T) {
	w := nativeWorld(t)
	w.must(nconv(""), session, "s1")
	w.handle["pa"] = reply(400, `{"type":"error"}`)
	if res := w.must(nconv(""), session, "s1"); res.Status != 400 || res.Affinity != "hit" {
		t.Fatalf("%+v", res)
	}
	w.handle["pa"] = claudeMessage(0)
	if res := w.must(nconv(""), session, "s1"); res.Affinity != "hit" {
		t.Fatalf("pin lost after a 400: %q", res.Affinity)
	}
	fresh := nativeWorld(t)
	fresh.handle["pa"], fresh.handle["pb"] = reply(400, `{}`), reply(400, `{}`)
	fresh.must(nconv(""), session, "s1")
	if fresh.pins() != 0 {
		t.Fatal("an error reply created a pin")
	}
}

func TestMessagesPinLifetime(t *testing.T) {
	t.Run("expires when idle and refreshes on use", func(t *testing.T) {
		w := nativeWorld(t)
		w.must(nconv(""), session, "s1")
		w.clk.advance(500 * time.Second)
		w.must(nconv(""), session, "s1")
		w.clk.advance(500 * time.Second)
		if res := w.must(nconv(""), session, "s1"); res.Affinity != "hit" {
			t.Fatalf("use did not refresh the pin: %q", res.Affinity)
		}
		w.clk.advance(601 * time.Second)
		if res := w.must(nconv(""), session, "s1"); res.Affinity != "new" {
			t.Fatalf("idle pin still live: %q", res.Affinity)
		}
	})
	for name, tc := range map[string]struct {
		body string
		long bool
	}{
		"block-level ttl":     {`{"model":"m","max_tokens":1,"system":[{"type":"text","text":"s","cache_control": {"type":"ephemeral", "ttl" : "1h"}}],"messages":[{"role":"user","content":"hi"}]}`, true},
		"top-level ttl":       {nconv(`"cache_control":{"type":"ephemeral","ttl":"1h"},`), true},
		"message block ttl":   {`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`, true},
		"5m ttl stays short":  {nconv(`"cache_control":{"type":"ephemeral","ttl":"5m"},`), false},
		"text mentioning ttl": {`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"{\"ttl\":\"1h\"}"}]}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			w := nativeWorld(t)
			w.must(tc.body, session, "s1")
			w.clk.advance(3000 * time.Second)
			if res := w.must(tc.body, session, "s1"); (res.Affinity == "hit") != tc.long {
				t.Fatalf("%q, long=%v", res.Affinity, tc.long)
			}
		})
	}
	t.Run("zero seconds never pins", func(t *testing.T) {
		w := nativeWorld(t)
		for _, id := range []string{"pa", "pb"} {
			p := w.e.providers.(fakeProviders)[id]
			p.CacheAffinitySeconds = 0
			w.e.providers.(fakeProviders)[id] = p
		}
		w.must(nconv(""), session, "s1")
		w.must(nconv(""), session, "s1")
		if want(t, w, "ab"); w.pins() != 0 {
			t.Fatal("pinned a provider with 0 seconds")
		}
	})
}

func TestMessagesAffinityOffSpreads(t *testing.T) {
	w := nativeWorld(t)
	w.e.models = fakeModels(virtualmodels.Model{
		Name: "m", Kind: virtualmodels.KindChat, Strategy: virtualmodels.StrategyRoundRobin,
		Tiers: []virtualmodels.Tier{{Entries: []virtualmodels.Entry{ent("a", "pa", 1), ent("b", "pb", 1)}}},
	})
	w.must(nconv(""), session, "s1")
	res := w.must(nconv(""), session, "s1")
	if want(t, w, "ab"); res.Affinity != "" || w.pins() != 0 {
		t.Fatalf("label %q pins %d", res.Affinity, w.pins())
	}
}

func TestMessagesBodyAndHeadersUntouchedWithPins(t *testing.T) {
	const client = `{"cache_control": {"type":"ephemeral"},"max_tokens":10,"messages":[{"content":"hi","role":"user"}],"model":"m","session_id":"keep","stream":false}`
	w := nativeWorld(t)
	w.must(client, session, "s1")
	w.must(client, session, "s1")
	var in, out map[string]json.RawMessage
	_ = json.Unmarshal([]byte(client), &in)
	_ = json.Unmarshal([]byte(w.bodies["pa"]), &out)
	delete(in, "model")
	delete(out, "model")
	if len(in) != len(out) {
		t.Fatalf("fields added or removed: %s", w.bodies["pa"])
	}
	for k, v := range in {
		if string(out[k]) != string(v) {
			t.Fatalf("%s changed: %s", k, out[k])
		}
	}
	for _, h := range []string{"X-Session-Affinity", "Prompt-Cache-Key"} {
		if w.reqs["pa"].Header.Get(h) != "" {
			t.Fatalf("injected header %s", h)
		}
	}
}

func TestClaudeCacheTeeKeepsBytesAndFiresOnce(t *testing.T) {
	start := `event: message_start` + "\n" + `data: {"type":"message_start","message":{"usage":{"cache_read_input_tokens":5}}}` + "\n\n"
	input := "event: ping\ndata: {\"a\":1}\n\n" + start + start + "event: message_stop\ndata: {}\n\n"
	fired := 0
	tee := watchCache(io.NopCloser(oneByte{strings.NewReader(input)}), claudeCacheRead, func() { fired++ })
	got, err := io.ReadAll(tee)
	if err != nil || string(got) != input || fired != 1 {
		t.Fatalf("err=%v fired=%d identical=%v", err, fired, string(got) == input)
	}
}
