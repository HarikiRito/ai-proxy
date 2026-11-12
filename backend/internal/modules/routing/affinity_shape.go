package routing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
)

// affinityShape is how affinity reads one body format: conversation id, opening hash, 1h request, cached reply.
type affinityShape struct {
	explicitID func(http.Header, map[string]json.RawMessage) string
	opening    func(map[string]json.RawMessage) (string, bool)
	oneHour    func(map[string]json.RawMessage) bool
	cached     func([]byte) bool
}

var chatShape = affinityShape{
	explicitID: chatExplicitID,
	opening:    openingHash,
	oneHour: func(body map[string]json.RawMessage) bool {
		var cc struct{ TTL string }
		_ = json.Unmarshal(body["cache_control"], &cc)
		return cc.TTL == "1h"
	},
	cached: cachedTokens,
}

// ttl 1h may sit on any cache_control block of a native body; escaped quotes inside text never match.
var oneHourTTL = regexp.MustCompile(`"ttl"\s*:\s*"1h"`)

var messagesShape = affinityShape{
	explicitID: func(hdr http.Header, _ map[string]json.RawMessage) string { return truncateID(hdr.Get("X-Session-Id")) },
	opening:    messagesOpening,
	oneHour: func(body map[string]json.RawMessage) bool {
		for _, v := range body {
			if oneHourTTL.Match(v) {
				return true
			}
		}
		return false
	},
	cached: claudeCacheRead,
}

// messagesOpening identifies a native conversation by its system prompt and first message.
func messagesOpening(body map[string]json.RawMessage) (string, bool) {
	var msgs []struct {
		Role    string
		Content json.RawMessage
	}
	if json.Unmarshal(body["messages"], &msgs) != nil || len(msgs) == 0 {
		return "", false
	}
	h := sha256.New()
	h.Write(body["system"])
	h.Write([]byte("\x00" + msgs[0].Role + "\x00"))
	h.Write(msgs[0].Content)
	return hex.EncodeToString(h.Sum(nil)), true
}

var cacheReadMarker = []byte("cache_read_input_tokens")

// claudeCacheRead reports whether a native reply, or a message_start/message_delta event, shows cache reads.
func claudeCacheRead(data []byte) bool {
	if !bytes.Contains(data, cacheReadMarker) {
		return false
	}
	var in struct {
		Usage   claudeUsage
		Message struct{ Usage claudeUsage }
	}
	return json.Unmarshal(data, &in) == nil && in.Usage.CacheReadInputTokens+in.Message.Usage.CacheReadInputTokens > 0
}
