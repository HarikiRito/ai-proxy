package routing

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

var usageMarker = []byte("usage")

func (u chatUsage) tokens() Usage {
	return Usage{In: &u.PromptTokens, Out: &u.CompletionTokens, CacheRead: &u.PromptTokensDetails.CachedTokens}
}

func (u claudeUsage) tokens() Usage {
	prompt := u.prompt()
	return Usage{In: &prompt, Out: &u.OutputTokens, CacheRead: &u.CacheReadInputTokens, CacheCreation: &u.CacheCreationInputTokens}
}

// openAIUsage reads the usage object of a chat completion or its final chunk.
func openAIUsage(data []byte) (Usage, bool) {
	var in struct{ Usage *chatUsage }
	if !bytes.Contains(data, usageMarker) || json.Unmarshal(data, &in) != nil || in.Usage == nil {
		return Usage{}, false
	}
	return in.Usage.tokens(), true
}

// claudeBodyUsage reads the usage object of a buffered Claude message.
func claudeBodyUsage(data []byte) (Usage, bool) {
	var in struct{ Usage *claudeUsage }
	if json.Unmarshal(data, &in) != nil || in.Usage == nil {
		return Usage{}, false
	}
	return in.Usage.tokens(), true
}

// meter reports a chat answer's token counts through res.Meta.Usage, before any translation.
// Buffered answers fill it now; streams fill it as the usage events pass, bytes untouched.
func (r *request) meter(a attempt, res Result) Result {
	if r.pl.kind != virtualmodels.KindChat || res.Status/100 != 2 {
		return res
	}
	claude := a.t.provider.Kind == providers.KindAnthropic
	u := &Usage{}
	res.Meta.Usage = u
	switch {
	case res.Stream == nil && claude:
		*u, _ = claudeBodyUsage(res.Body)
	case res.Stream == nil:
		*u, _ = openAIUsage(res.Body)
	case claude:
		res.Stream = watchLines(res.Stream, claudeStreamUsage(u))
	default:
		res.Stream = watchLines(res.Stream, func(data []byte) {
			if v, ok := openAIUsage(data); ok {
				*u = v
			}
		})
	}
	return res
}

// claudeStreamUsage follows message_start (input and cache) and message_delta (output) events.
func claudeStreamUsage(u *Usage) func([]byte) {
	var raw claudeUsage
	return func(data []byte) {
		var ev struct {
			Type    string
			Message struct{ Usage claudeUsage }
			Usage   claudeUsage
		}
		if !bytes.Contains(data, usageMarker) || json.Unmarshal(data, &ev) != nil {
			return
		}
		switch ev.Type {
		case "message_start":
			raw = ev.Message.Usage
		case "message_delta":
			raw.OutputTokens = ev.Usage.OutputTokens
		default:
			return
		}
		*u = raw.tokens()
	}
}

// watchLines hands every SSE data line of a passthrough stream to fn.
func watchLines(s io.ReadCloser, fn func([]byte)) io.ReadCloser {
	return &cacheTee{ReadCloser: s, cached: func(data []byte) bool { fn(data); return false }}
}
