package routing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/settings"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

type request struct {
	e         *Engine
	ctx       context.Context //nolint:containedctx // request-scoped state, lives for one Do call
	cancel    context.CancelFunc
	cfg       settings.Settings
	model     virtualmodels.Model
	chain     []tier
	hdr       http.Header
	pl        payload
	stream    bool
	ttl       time.Duration
	tried     map[string]bool
	last      error
	meta      Meta // identity of the latest attempt
	waitUntil time.Time
	aff       *affinity
}

// chain keeps only usable targets, tiers in ascending priority.
func (e *Engine) chain(ctx context.Context, m virtualmodels.Model, pl payload) ([]tier, error) {
	var ids []string
	for _, t := range m.Tiers {
		for _, en := range t.Entries {
			ids = append(ids, en.ProviderID)
		}
	}
	provs, err := e.providers.ByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	var chain []tier
	for _, t := range m.Tiers {
		cur := tier{key: fmt.Sprintf("%s#%d", m.Name, t.Priority)}
		for _, en := range t.Entries {
			p, ok := provs[en.ProviderID]
			if en.Status != virtualmodels.StatusOK || !ok || !p.Enabled || !slices.Contains(pl.accepts, p.Kind) {
				e.log.Debug("skipping target", "model", m.Name, "provider", en.ProviderID, "status", en.Status)
				continue
			}
			cur.targets = append(cur.targets, target{entry: en, provider: p})
		}
		if len(cur.targets) > 0 {
			chain = append(chain, cur)
		}
	}
	return chain, nil
}

func (r *request) run() (Result, error) {
	for {
		a, out, ok := r.pick()
		if !ok {
			if !out.untried {
				return Result{}, r.withMeta(r.exhausted())
			}
			if err := r.wait(out); err != nil {
				return Result{}, r.withMeta(err)
			}
			continue
		}
		r.attempted(a)
		res, err := r.call(a)
		if err == nil {
			usage := res.Meta.Usage
			res.Meta, res.Meta.Usage = r.meta, usage
			return r.served(a, res), nil
		}
		r.failed(a)
		if cerr := r.ctx.Err(); cerr != nil {
			return Result{}, r.withMeta(ctxError(cerr))
		}
		r.tried[pairKey(a.t, a.key)] = true
		r.last = err
	}
}

// attempted records the target about to be called.
func (r *request) attempted(a attempt) {
	r.meta = Meta{
		ProviderID: a.t.provider.ID, ProviderName: a.t.provider.Name, KeyID: a.key.ID, KeyName: a.key.Name,
		EntryID: a.t.entry.ID, Model: a.t.entry.Model, Steps: r.meta.Steps + 1,
	}
}

// withMeta attaches the attempts so far to an engine error; other errors pass through.
func (r *request) withMeta(err error) error {
	var re *Error
	if errors.As(err, &re) {
		re.Meta = r.meta
	}
	return err
}

func (r *request) exhausted() error {
	if r.last == nil {
		return &Error{Status: http.StatusServiceUnavailable, Code: "no_available_provider", Message: "no usable provider for this model"}
	}
	return &Error{Status: http.StatusBadGateway, Code: "upstream_error", Message: "all providers failed, last: " + r.last.Error()}
}

func ctxError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Status: http.StatusGatewayTimeout, Code: "timeout", Message: "request timed out"}
	}
	return err
}

// wait blocks until capacity may exist; once the queue budget is spent it answers 429.
func (r *request) wait(out outlook) error {
	if r.waitUntil.IsZero() {
		r.waitUntil = r.e.now().Add(time.Duration(r.cfg.QueueWaitSeconds) * time.Second)
	}
	remaining := r.waitUntil.Sub(r.e.now())
	if remaining <= 0 {
		secs := max(int((out.soonest+time.Second-1)/time.Second), 1)
		return &Error{Status: http.StatusTooManyRequests, Code: "rate_limit_exceeded", Message: "all providers are busy, retry later", RetryAfter: secs}
	}
	if out.soonest > 0 {
		remaining = min(remaining, out.soonest)
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return ctxError(r.ctx.Err())
	case <-out.changed:
	case <-timer.C:
	}
	return nil
}
