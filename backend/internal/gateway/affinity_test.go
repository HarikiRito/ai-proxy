package gateway

import (
	"net/http"
	"testing"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

// affine spreads model "m" round robin over the providers, with cache affinity on.
func (e *env) affine(ids ...string) {
	e.route(ids)
	m := e.models["m"]
	m.Strategy, m.CacheAffinity = virtualmodels.StrategyRoundRobin, true
	e.models["m"] = m
	for _, id := range ids {
		p := e.provs[id]
		p.CacheAffinitySeconds = 600
		e.provs[id] = p
	}
}

func (e *env) turns(t *testing.T, n int, session string) {
	t.Helper()
	for range n {
		resp, body := e.do(t, http.MethodPost, "/v1/chat/completions", simple, map[string]string{"Authorization": "Bearer " + e.token, "X-Session-Id": session})
		if resp.StatusCode != 200 {
			t.Fatalf("%d %s", resp.StatusCode, body)
		}
	}
}

func TestConversationStaysOnOneTarget(t *testing.T) {
	e := newEnv(t)
	a := e.upstream(t, "a", 9, ok(`{}`))
	b := e.upstream(t, "b", 9, ok(`{}`))
	e.affine("a", "b")
	e.turns(t, 20, "conv-1")
	if got := a.Load() + b.Load(); got != 20 || (a.Load() != 0 && b.Load() != 0) {
		t.Fatalf("a=%d b=%d", a.Load(), b.Load())
	}
}

func TestPinnedUpstreamDiesConversationMovesAndRepins(t *testing.T) {
	e := newEnv(t)
	broken := false
	a := e.upstream(t, "a", 9, func(w http.ResponseWriter, r *http.Request) {
		if broken {
			status(http.StatusBadGateway)(w, r)
			return
		}
		ok(`{}`)(w, r)
	})
	b := e.upstream(t, "b", 9, ok(`{}`))
	e.affine("a", "b")
	e.turns(t, 3, "conv-1")
	if a.Load() != 3 {
		t.Fatalf("conversation did not start on a: a=%d b=%d", a.Load(), b.Load())
	}
	broken = true
	e.turns(t, 5, "conv-1")
	if a.Load() != 4 || b.Load() != 5 {
		t.Fatalf("after the kill a=%d b=%d, want 4 and 5", a.Load(), b.Load())
	}
}

func TestRestartRoutesAnew(t *testing.T) {
	e := newEnv(t)
	e.upstream(t, "a", 9, ok(`{}`))
	e.upstream(t, "b", 9, ok(`{}`))
	e.affine("a", "b")
	e.turns(t, 3, "conv-1")
	e.serve(t)
	e.turns(t, 3, "conv-1")
}
