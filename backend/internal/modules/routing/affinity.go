package routing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
)

const (
	maxExplicitID = 256
	oneHourLife   = time.Hour
)

type affinityCtx struct{}

// WithAffinity scopes cache-affinity pins to one proxy key; without it a request is never pinned.
func WithAffinity(ctx context.Context, proxyKeyID string) context.Context {
	return context.WithValue(ctx, affinityCtx{}, proxyKeyID)
}

// affinity is one request's view of its conversation pin.
type affinity struct {
	key      string
	explicit bool // the client named the conversation
	hinted   bool // the client already sent a cache hint
	oneHour  bool
	start    *pin // pin found when the request arrived
	live     *pin // start until its target fails
}

func (e *Engine) newAffinity(ctx context.Context, model string, hdr http.Header, pl payload) *affinity {
	scope, ok := ctx.Value(affinityCtx{}).(string)
	if !ok || scope == "" {
		return nil
	}
	body := pl.body
	id := pl.shape.explicitID(hdr, body)
	explicit := id != ""
	if !explicit {
		if id, ok = pl.shape.opening(body); !ok {
			return nil
		}
	}
	a := &affinity{key: scope + "|" + model + "|" + id, explicit: explicit, hinted: hasHint(hdr, body), oneHour: pl.shape.oneHour(body)}
	if p, found := e.pins.get(a.key, e.now()); found {
		a.start, a.live = &p, &p
	}
	return a
}

// chatExplicitID is the client's own conversation id: body session_id, header x-session-id, body prompt_cache_key.
func chatExplicitID(hdr http.Header, body map[string]json.RawMessage) string {
	var id string
	_ = json.Unmarshal(body["session_id"], &id)
	if id == "" {
		id = hdr.Get("X-Session-Id")
	}
	if id == "" {
		_ = json.Unmarshal(body["prompt_cache_key"], &id)
	}
	return truncateID(id)
}

func truncateID(id string) string {
	if utf8.RuneCountInString(id) > maxExplicitID {
		id = string([]rune(id)[:maxExplicitID])
	}
	return id
}

func hasHint(hdr http.Header, body map[string]json.RawMessage) bool {
	return hdr.Get("X-Session-Id") != "" || present(body["session_id"]) || present(body["prompt_cache_key"])
}

// openingHash identifies a conversation by its first system prompt and first non-system message.
func openingHash(body map[string]json.RawMessage) (string, bool) {
	var msgs []chatTurn
	if json.Unmarshal(body["messages"], &msgs) != nil {
		return "", false
	}
	h, sawSystem := sha256.New(), false
	for _, m := range msgs {
		if m.Role == "system" || m.Role == "developer" {
			if !sawSystem {
				h.Write([]byte(m.systemText()))
				sawSystem = true
			}
			continue
		}
		var content bytes.Buffer
		if json.Compact(&content, m.Content) != nil {
			content.Reset()
			content.Write(m.Content)
		}
		h.Write([]byte("\x00" + m.Role + "\x00"))
		h.Write(content.Bytes())
		return hex.EncodeToString(h.Sum(nil)), true
	}
	return "", false
}

func (m chatTurn) systemText() string {
	blocks, ok := m.blocks()
	if !ok {
		return string(m.Content)
	}
	var parts []string
	for _, b := range blocks {
		if b.Text != nil {
			parts = append(parts, *b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// lifetime is how long a pin on this provider lasts; 0 = no pinning there.
func (a *affinity) lifetime(p providers.Provider) time.Duration {
	d := time.Duration(p.CacheAffinitySeconds) * time.Second
	if d > 0 && a.oneHour {
		d = max(d, oneHourLife)
	}
	return d
}

// label tells how the serving target relates to the pin seen on arrival.
func (a *affinity) label(t attempt) string {
	switch {
	case a.start == nil:
		return "new"
	case a.start.is(t):
		return "hit"
	}
	return "break"
}

// served finishes a delivered upstream answer: labels it and updates the pin by the explicit/cached rules.
func (r *request) served(a attempt, res Result) Result {
	af := r.aff
	if af == nil {
		return res
	}
	res.Affinity = af.label(a)
	repin := func() { r.repin(a) }
	switch {
	case res.Status/100 != 2:
	case af.explicit:
		repin()
	case res.Stream == nil:
		if r.pl.shape.cached(res.Body) {
			repin()
		}
	default:
		res.Stream = watchCache(res.Stream, r.pl.shape.cached, repin)
	}
	return res
}

func (r *request) repin(a attempt) {
	if d := r.aff.lifetime(a.t.provider); d > 0 {
		r.e.pins.set(r.aff.key, pin{entryID: a.t.entry.ID, keyID: a.key.ID, expires: r.e.now().Add(d)})
	}
}

// failed drops the pin when its own target failed; a client exit says nothing about the target.
func (r *request) failed(a attempt) {
	af := r.aff
	if af == nil || af.live == nil || !af.live.is(a) {
		return
	}
	if err := r.ctx.Err(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return
	}
	r.e.pins.drop(af.key, *af.live)
	af.live = nil
}

// pickPinned leases a key of the pinned entry: the pinned key if it has room, else another key by weight.
// The caller holds e.mu.
func (r *request) pickPinned(now time.Time, out *outlook) (attempt, bool) {
	p := r.aff.live
	if p == nil {
		return attempt{}, false
	}
	for _, t := range r.chain {
		for _, c := range r.candidates(t, now, out) {
			if c.t.entry.ID != p.entryID || r.aff.lifetime(c.t.provider) == 0 {
				continue
			}
			var k providers.Key
			if i := slices.IndexFunc(c.keys, func(k providers.Key) bool { return k.ID == p.keyID }); i >= 0 {
				k = c.keys[i]
			} else {
				k = r.e.chooseKey(c.keys)
			}
			return attempt{lease: r.e.take(k.ID, r.ttl), key: k, t: c.t}, true
		}
	}
	return attempt{}, false
}
