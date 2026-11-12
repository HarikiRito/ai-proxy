package routing

import (
	"context"
	"maps"
	"sort"
	"time"
)

// KeyStatus is the live state of one provider key.
type KeyStatus struct {
	ProviderID, ProviderName string
	ProviderEnabled          bool
	KeyID, KeyName           string
	KeyEnabled               bool
	MaxInFlight, InFlight    int
	Free                     int
	CooldownUntil            *time.Time // nil unless the key is cooling down
	Flag                     string     // flagNeedsLogin, flagExpired, or "" when the key is fine
}

// Enabled reports whether both the provider and the key are enabled.
func (k KeyStatus) Enabled() bool { return k.ProviderEnabled && k.KeyEnabled }

// Snapshot lists every provider key with its in-flight count and cooldown, ordered by provider then key name.
func (e *Engine) Snapshot(ctx context.Context) ([]KeyStatus, error) {
	provs, err := e.providers.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	now := e.now()
	inflight := make(map[string]int, len(e.inflight))
	for id, n := range e.inflight {
		inflight[id] = n
	}
	cooling := make(map[string]time.Time, len(e.cooldown))
	for id, until := range e.cooldown {
		if until.After(now) {
			cooling[id] = until
		}
	}
	flagged := maps.Clone(e.flags)
	e.mu.Unlock()

	var out []KeyStatus
	for _, p := range provs {
		for _, k := range p.Keys {
			st := KeyStatus{
				ProviderID: p.ID, ProviderName: p.Name, ProviderEnabled: p.Enabled,
				KeyID: k.ID, KeyName: k.Name, KeyEnabled: k.Enabled,
				MaxInFlight: k.MaxInFlight, InFlight: inflight[k.ID], Free: max(k.MaxInFlight-inflight[k.ID], 0),
			}
			st.Flag = keyFlag(k, now, flagged)
			if until, ok := cooling[k.ID]; ok {
				st.CooldownUntil = &until
			}
			out = append(out, st)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ProviderName != out[j].ProviderName {
			return out[i].ProviderName < out[j].ProviderName
		}
		return out[i].KeyName < out[j].KeyName
	})
	return out, nil
}
