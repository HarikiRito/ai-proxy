// Package settings owns runtime knobs the owner can change without a redeploy. A missing row means defaults.
package settings

import (
	"context"
	"fmt"
	"sync"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
)

const (
	DefaultMaxBodyBytes           = 25 << 20
	DefaultRequestTimeoutSeconds  = 600
	DefaultQueueWaitSeconds       = 30
	DefaultDefaultCooldownSeconds = 60

	maxBodyBytesLimit   = 1 << 30
	maxTimeoutSeconds   = 86400
	maxQueueWaitSeconds = 600
	maxCooldownSeconds  = 86400
)

type Deps struct {
	DB *ent.Client
}

type Settings struct {
	MaxBodyBytes           int
	RequestTimeoutSeconds  int
	QueueWaitSeconds       int
	DefaultCooldownSeconds int
}

// Input updates only the fields given (nil = unchanged).
type Input struct {
	MaxBodyBytes           *int
	RequestTimeoutSeconds  *int
	QueueWaitSeconds       *int
	DefaultCooldownSeconds *int
}

// UserError is safe to show to the owner (validation).
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userError(msg string) error { return &UserError{Msg: msg} }

func defaults() Settings {
	return Settings{
		MaxBodyBytes:           DefaultMaxBodyBytes,
		RequestTimeoutSeconds:  DefaultRequestTimeoutSeconds,
		QueueWaitSeconds:       DefaultQueueWaitSeconds,
		DefaultCooldownSeconds: DefaultDefaultCooldownSeconds,
	}
}

type Service struct {
	store *store

	mu     sync.Mutex
	cached *Settings
}

func New(d Deps) *Service {
	return &Service{store: &store{db: d.DB}}
}

// Get is cached in memory until the next Update (single process, so no cross-instance invalidation).
func (s *Service) Get(ctx context.Context) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(ctx)
}

func (s *Service) Update(ctx context.Context, in Input) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.load(ctx)
	if err != nil {
		return Settings{}, err
	}
	next := merge(cur, in)
	if err := validate(next); err != nil {
		return Settings{}, err
	}
	if err := s.store.save(ctx, next); err != nil {
		return Settings{}, err
	}
	s.cached = &next
	return next, nil
}

func (s *Service) load(ctx context.Context) (Settings, error) {
	if s.cached != nil {
		return *s.cached, nil
	}
	cur, err := s.store.get(ctx)
	if err != nil {
		return Settings{}, err
	}
	s.cached = &cur
	return cur, nil
}

func merge(cur Settings, in Input) Settings {
	set := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	set(&cur.MaxBodyBytes, in.MaxBodyBytes)
	set(&cur.RequestTimeoutSeconds, in.RequestTimeoutSeconds)
	set(&cur.QueueWaitSeconds, in.QueueWaitSeconds)
	set(&cur.DefaultCooldownSeconds, in.DefaultCooldownSeconds)
	return cur
}

func validate(s Settings) error {
	for _, f := range []struct {
		name       string
		val, upper int
	}{
		{"max body bytes", s.MaxBodyBytes, maxBodyBytesLimit},
		{"request timeout seconds", s.RequestTimeoutSeconds, maxTimeoutSeconds},
		{"queue wait seconds", s.QueueWaitSeconds, maxQueueWaitSeconds},
		{"default cooldown seconds", s.DefaultCooldownSeconds, maxCooldownSeconds},
	} {
		if f.val < 1 || f.val > f.upper {
			return userError(fmt.Sprintf("%s must be between 1 and %d", f.name, f.upper))
		}
	}
	return nil
}
