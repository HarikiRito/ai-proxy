package routing

import (
	"context"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

type lease struct {
	keyID   string
	expires time.Time
}

type target struct {
	entry    virtualmodels.Entry
	provider providers.Provider
}

type tier struct {
	key     string
	targets []target
}

type attempt struct {
	lease uint64
	key   providers.Key
	t     target
}

// outlook describes why nothing was picked: whether untried keys remain and when the soonest cooldown ends.
type outlook struct {
	changed <-chan struct{}
	untried bool
	soonest time.Duration // 0 = no cooldown pending
}

type candidate struct {
	idx  int // position in the tier
	t    target
	keys []providers.Key
	free int
}

// take records a lease; the caller holds e.mu and has checked capacity.
func (e *Engine) take(keyID string, ttl time.Duration) uint64 {
	e.lastID++
	e.inflight[keyID]++
	e.leases[e.lastID] = lease{keyID: keyID, expires: e.now().Add(ttl)}
	return e.lastID
}

// release is idempotent: the watchdog may already have freed the lease.
func (e *Engine) release(id uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if l, ok := e.leases[id]; ok {
		e.drop(id, l)
		e.notify()
	}
}

func (e *Engine) drop(id uint64, l lease) {
	delete(e.leases, id)
	if e.inflight[l.keyID]--; e.inflight[l.keyID] <= 0 {
		delete(e.inflight, l.keyID)
	}
}

// notify wakes queued requests; the caller holds e.mu.
func (e *Engine) notify() {
	close(e.changed)
	e.changed = make(chan struct{})
}

// sweep frees leases older than their ttl.
func (e *Engine) sweep() {
	e.mu.Lock()
	now, freed := e.now(), 0
	for id, l := range e.leases {
		if now.After(l.expires) {
			e.drop(id, l)
			freed++
		}
	}
	if freed > 0 {
		e.notify()
	}
	e.mu.Unlock()
	if freed > 0 {
		e.log.Warn("freed expired leases", "count", freed)
	}
}

func (e *Engine) cool(keyID string, d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if until := e.now().Add(d); until.After(e.cooldown[keyID]) {
		e.cooldown[keyID] = until
	}
	e.notify()
}

// Watch frees leaked leases until ctx ends.
func (e *Engine) Watch(ctx context.Context) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.sweep()
		}
	}
}

// pick leases one untried key following tier order and strategy; atomic, so limits are never exceeded.
func (r *request) pick() (attempt, outlook, bool) {
	e := r.e
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	out := outlook{changed: e.changed}
	for _, t := range r.chain {
		cands := r.candidates(t, now, &out)
		if len(cands) == 0 {
			continue
		}
		c := cands[e.choose(r.model.Strategy, t, cands)]
		k := e.chooseKey(c.keys)
		return attempt{lease: e.take(k.ID, r.ttl), key: k, t: c.t}, out, true
	}
	return attempt{}, out, false
}

// candidates lists the tier's targets that have an untried key ready now; it records waiting hints in out.
func (r *request) candidates(t tier, now time.Time, out *outlook) []candidate {
	e := r.e
	var cands []candidate
	for i, tg := range t.targets {
		c := candidate{idx: i, t: tg}
		for _, k := range tg.provider.Keys {
			if room := e.room(k, r.tried[pairKey(tg, k)], now, out); room > 0 {
				c.keys = append(c.keys, k)
				c.free += room
			}
		}
		if len(c.keys) > 0 {
			cands = append(cands, c)
		}
	}
	return cands
}

// room is the free slots of a key this request may still use now; it records waiting hints in out.
func (e *Engine) room(k providers.Key, tried bool, now time.Time, out *outlook) int {
	if !k.Enabled || tried {
		return 0
	}
	out.untried = true
	if until, ok := e.cooldown[k.ID]; ok && until.After(now) {
		out.noteCooldown(until.Sub(now))
		return 0
	}
	delete(e.cooldown, k.ID)
	return max(k.MaxInFlight-e.inflight[k.ID], 0)
}

func (o *outlook) noteCooldown(d time.Duration) {
	if o.soonest == 0 || d < o.soonest {
		o.soonest = d
	}
}

func pairKey(t target, k providers.Key) string { return t.entry.ID + "/" + k.ID }

// choose returns the index in cands of the target to use.
func (e *Engine) choose(s virtualmodels.Strategy, t tier, cands []candidate) int {
	switch s {
	case virtualmodels.StrategyRoundRobin:
		start, size := e.cursor[t.key], len(t.targets)
		best, bestDist := 0, size
		for i, c := range cands {
			if d := (c.idx - start%size + size) % size; d < bestDist {
				best, bestDist = i, d
			}
		}
		e.cursor[t.key] = cands[best].idx + 1
		return best
	case virtualmodels.StrategyWeightedRoundRobin:
		ids, weights := make([]string, len(cands)), make([]int, len(cands))
		for i, c := range cands {
			ids[i], weights[i] = t.key+"/"+c.t.entry.ID, c.t.entry.Weight
		}
		return e.smooth(ids, weights)
	case virtualmodels.StrategyLeastInFlight:
		best := 0
		for i, c := range cands {
			if c.free > cands[best].free {
				best = i
			}
		}
		return best
	}
	return 0 // priority_overflow: first with room
}

func (e *Engine) chooseKey(keys []providers.Key) providers.Key {
	ids, weights := make([]string, len(keys)), make([]int, len(keys))
	for i, k := range keys {
		ids[i], weights[i] = "key/"+k.ID, k.Weight
	}
	return keys[e.smooth(ids, weights)]
}

// smooth is smooth weighted round robin: deterministic and evenly interleaved.
func (e *Engine) smooth(ids []string, weights []int) int {
	total, best := 0, 0
	for i, id := range ids {
		w := max(weights[i], 1)
		total += w
		e.credit[id] += w
		if e.credit[id] > e.credit[ids[best]] {
			best = i
		}
	}
	e.credit[ids[best]] -= total
	return best
}
