package routing

import (
	"bytes"
	"encoding/json"
	"io"
)

const maxScanLine = 1 << 20

var cachedMarker = []byte("cached_tokens")

// cachedTokens reports whether a chat completion (or chunk) body shows cached prompt tokens.
func cachedTokens(data []byte) bool {
	if !bytes.Contains(data, cachedMarker) {
		return false
	}
	var in struct{ Usage chatUsage }
	return json.Unmarshal(data, &in) == nil && in.Usage.PromptTokensDetails.CachedTokens > 0
}

// watchCache calls seen once when the stream shows cached prompt tokens; the bytes are never altered.
func watchCache(s io.ReadCloser, seen func()) io.ReadCloser {
	if cs, ok := s.(*chatStream); ok {
		cs.onCached = seen
		return cs
	}
	return &cacheTee{ReadCloser: s, seen: seen}
}

// cacheTee scans SSE data lines of a passthrough stream.
type cacheTee struct {
	io.ReadCloser
	seen func()
	buf  []byte
	done bool
}

func (t *cacheTee) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if n > 0 && !t.done {
		t.scan(p[:n])
	}
	return n, err
}

func (t *cacheTee) scan(b []byte) {
	t.buf = append(t.buf, b...)
	for !t.done {
		line, rest, found := bytes.Cut(t.buf, []byte("\n"))
		if !found {
			break
		}
		if data, ok := bytes.CutPrefix(line, []byte("data:")); ok && cachedTokens(data) {
			t.done = true
			t.seen()
		}
		t.buf = rest
	}
	if t.done || len(t.buf) > maxScanLine {
		t.buf = nil
	}
}
