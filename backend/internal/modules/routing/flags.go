package routing

import (
	"net/http"
	"strings"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
)

const (
	flagNeedsLogin = "needs_login"
	flagExpired    = "expired"
	flagRetryAfter = 10 * time.Minute
)

// keyFlag says why a subscription key is held back; flagged maps key id to the time of its last 401 or 403.
func keyFlag(k providers.Key, now time.Time, flagged map[string]time.Time) string {
	if k.TokenExpiresAt != nil && !now.Before(*k.TokenExpiresAt) {
		return flagExpired
	}
	if _, ok := flagged[k.ID]; ok {
		return flagNeedsLogin
	}
	return ""
}

// blocked reports whether a key must be skipped now; a flagged key is probed again once its flag is old enough.
// The caller holds e.mu.
func (e *Engine) blocked(k providers.Key, now time.Time) bool {
	switch keyFlag(k, now, e.flags) {
	case flagExpired:
		return true
	case flagNeedsLogin:
		return now.Sub(e.flags[k.ID]) < flagRetryAfter
	}
	return false
}

func (e *Engine) flagKey(keyID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.flags[keyID] = e.now()
}

func (e *Engine) unflagKey(keyID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.flags, keyID)
}

// ignoredNames are sampling settings the subscription sidecar drops, sorted.
var ignoredNames = []string{"max_completion_tokens", "max_tokens", "stop", "stop_sequences", "temperature", "top_k", "top_p"}

// ignoredParams is the x-proxy-ignored-params header when a subscription target serves a body that sets any of them.
func (r *request) ignoredParams(a attempt) http.Header {
	if a.t.provider.Kind != providers.KindClaudeSubscription {
		return nil
	}
	var sent []string
	for _, n := range ignoredNames {
		if present(r.pl.body[n]) {
			sent = append(sent, n)
		}
	}
	if len(sent) == 0 {
		return nil
	}
	return http.Header{"X-Proxy-Ignored-Params": {strings.Join(sent, ",")}}
}
