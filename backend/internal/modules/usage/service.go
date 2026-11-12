// Package usage owns the log of proxied requests: metadata and token counts only, never request or response content.
package usage

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

type Deps struct {
	DB  *ent.Client
	Log *slog.Logger     // optional
	Now func() time.Time // optional
}

// Record is one proxied request. Nil optionals are unknown or not applicable.
// InputTokens is the whole prompt, so cache reads and cache creation are parts of it.
type Record struct {
	ID                  string
	CreatedAt           time.Time
	ProxyKeyID          string
	ProxyKeyName        string
	VirtualModel        string
	ProviderID          *string
	ProviderName        *string
	KeyID               *string
	KeyName             *string
	Model               *string
	Status              int
	DurationMs          int
	InputTokens         *int
	OutputTokens        *int
	CacheReadTokens     *int
	CacheCreationTokens *int
	Steps               int
	Affinity            *string
}

type Filter struct {
	ProxyKeyID   *string
	VirtualModel *string
	From, To     *time.Time // inclusive
}

type Group string

const (
	GroupDay      Group = "DAY"
	GroupModel    Group = "MODEL"
	GroupProvider Group = "PROVIDER"
)

// Total sums one group; missing token counts add as 0 and Errors counts statuses of 400 and above.
type Total struct {
	Label               string
	Requests            int
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	Errors              int
}

type Page struct {
	Items []Record
	Total int
}

// UserError is safe to show to the owner (invalid query input).
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userError(msg string) error { return &UserError{Msg: msg} }

type Service struct {
	store  *store
	writer *writer
	now    func() time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	jobs   sync.WaitGroup
}

// New starts the writer; Close must run before the ent client closes.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	st := &store{db: d.DB}
	return &Service{store: st, writer: startWriter(st, d.Log, d.Now, flushEvery, flushBatch), now: d.Now}
}

// Record queues the record for writing and never blocks; a full queue drops it.
func (s *Service) Record(r Record) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = s.now()
	}
	s.writer.enqueue(r)
}

// Close flushes queued records and stops the background jobs; it is safe to call twice.
func (s *Service) Close() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.jobs.Wait()
	s.writer.close()
}

// Records returns one page, newest first; limit 0 means the default of 50.
func (s *Service) Records(ctx context.Context, f Filter, limit, offset int) (Page, error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 0 || limit > maxLimit || offset < 0 {
		return Page{}, userError("limit must be between 1 and 200 and offset must not be negative")
	}
	if err := validate(f); err != nil {
		return Page{}, err
	}
	return s.store.page(ctx, f, limit, offset)
}

// Totals sums the matching records per group.
func (s *Service) Totals(ctx context.Context, f Filter, by Group) ([]Total, error) {
	if err := validate(f); err != nil {
		return nil, err
	}
	return s.store.totals(ctx, f, by)
}

func validate(f Filter) error {
	if f.From != nil && f.To != nil && f.From.After(*f.To) {
		return userError("from must not be after to")
	}
	return nil
}
