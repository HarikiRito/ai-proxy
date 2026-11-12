package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
)

// hintField is where a known host reads its cache hint: a body field or a header.
type hintField struct{ body, header string }

func hintFieldFor(baseURL string) (hintField, bool) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return hintField{}, false
	}
	switch host := strings.ToLower(u.Hostname()); {
	case host == "api.openai.com":
		return hintField{body: "prompt_cache_key"}, true
	case inDomain(host, "fireworks.ai"):
		return hintField{header: "X-Session-Affinity"}, true
	case inDomain(host, "openrouter.ai"):
		return hintField{body: "session_id"}, true
	}
	return hintField{}, false
}

func inDomain(host, domain string) bool { return host == domain || strings.HasSuffix(host, "."+domain) }

// hint is the cache hint to add for this target: only when the client sent none, on a known host.
func (r *request) hint(a attempt) (hintField, string, bool) {
	af, p := r.aff, a.t.provider
	if af == nil || af.hinted || p.Kind != providers.KindOpenAICompatible || af.lifetime(p) == 0 {
		return hintField{}, "", false
	}
	f, ok := hintFieldFor(p.BaseURL)
	sum := sha256.Sum256([]byte(af.key))
	return f, hex.EncodeToString(sum[:]), ok
}

func (r *request) setHintHeader(h http.Header, a attempt) {
	if f, v, ok := r.hint(a); ok && f.header != "" && h.Get(f.header) == "" {
		h.Set(f.header, v)
	}
}

// body encodes the request for the target, adding a body hint when one applies.
func (r *request) body(a attempt) ([]byte, string) {
	if f, v, ok := r.hint(a); ok && f.body != "" {
		b := maps.Clone(r.pl.body)
		b[f.body], _ = json.Marshal(v)
		return withModel(b, a.t.entry.Model), "application/json"
	}
	return r.pl.build(a.t.entry.Model, a.t.provider.Kind)
}
