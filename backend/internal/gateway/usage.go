package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/proxykeys"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/routing"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/usage"
)

const (
	statusClientClosed = 499
	maxModelName       = 256 // the name is client input until the engine accepts it
)

// recorded logs every call that reaches the engine; streams are logged when closed.
func (g *gateway) recorded(call callFn) callFn {
	return func(ctx context.Context, name string, hdr http.Header, body map[string]json.RawMessage) (routing.Result, error) {
		return g.observe(ctx, name, func(ctx context.Context) (routing.Result, error) { return call(ctx, name, hdr, body) })
	}
}

func (g *gateway) observe(ctx context.Context, model string, call func(context.Context) (routing.Result, error)) (routing.Result, error) {
	if g.Recorder == nil {
		return call(ctx)
	}
	start := time.Now()
	key := ctx.Value(keyCtx{}).(proxykeys.Key)
	res, err := call(ctx)
	finish := func(status int, meta routing.Meta, affinity string) {
		g.Recorder.Record(newRecord(key, model, status, time.Since(start), meta, affinity))
	}
	var re *routing.Error
	switch {
	case errors.As(err, &re):
		finish(re.Status, re.Meta, "")
	case errors.Is(err, context.Canceled):
		finish(statusClientClosed, routing.Meta{}, "")
	case err != nil:
		finish(http.StatusInternalServerError, routing.Meta{}, "")
	case res.Stream != nil:
		res.Stream = &recordedStream{ReadCloser: res.Stream, done: func() { finish(res.Status, res.Meta, res.Affinity) }}
	default:
		finish(res.Status, res.Meta, res.Affinity)
	}
	return res, err
}

func newRecord(key proxykeys.Key, model string, status int, took time.Duration, m routing.Meta, affinity string) usage.Record {
	r := usage.Record{
		ProxyKeyID: key.ID, ProxyKeyName: key.Name, VirtualModel: model[:min(len(model), maxModelName)], Status: status, DurationMs: int(took.Milliseconds()), Steps: m.Steps,
		ProviderID: unlessEmpty(m.ProviderID), ProviderName: unlessEmpty(m.ProviderName), KeyID: unlessEmpty(m.KeyID), KeyName: unlessEmpty(m.KeyName),
		Model: unlessEmpty(m.Model), Affinity: unlessEmpty(affinity),
	}
	if u := m.Usage; u != nil {
		r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheCreationTokens = u.In, u.Out, u.CacheRead, u.CacheCreation
	}
	return r
}

func unlessEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// recordedStream runs done once after the first Close, when the stream's token counts are final.
type recordedStream struct {
	io.ReadCloser
	once sync.Once
	done func()
}

func (s *recordedStream) Close() error {
	err := s.ReadCloser.Close()
	s.once.Do(s.done)
	return err
}
