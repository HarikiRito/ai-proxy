// Package virtualmodels owns client-facing model names routed to provider entries in priority tiers.
package virtualmodels

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
)

type Kind string

const (
	KindChat          Kind = "chat"
	KindTranscription Kind = "transcription"
)

type Strategy string

const (
	StrategyRoundRobin         Strategy = "round_robin"
	StrategyWeightedRoundRobin Strategy = "weighted_round_robin"
	StrategyPriorityOverflow   Strategy = "priority_overflow"
	StrategyLeastInFlight      Strategy = "least_in_flight"
)

type Status string

const (
	StatusOK                   Status = "ok"
	StatusProviderMissing      Status = "provider_missing"
	StatusProviderDisabled     Status = "provider_disabled"
	StatusProviderIncompatible Status = "provider_incompatible"
)

// ProviderInfo is what this module needs to know about a provider; Kind is the provider kind as a string.
type ProviderInfo struct {
	Name    string
	Kind    string
	Enabled bool
}

// ProviderDirectory is implemented by the composition root; unknown ids are absent from the result.
type ProviderDirectory interface {
	ProviderInfo(ctx context.Context, ids []string) (map[string]ProviderInfo, error)
}

type Deps struct {
	DB        *ent.Client
	Providers ProviderDirectory
}

type Model struct {
	ID            string
	Name          string
	Kind          Kind
	Strategy      Strategy
	CacheAffinity bool
	Tiers         []Tier
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Tier priorities are contiguous from 0; lower is tried first.
type Tier struct {
	Priority int
	Entries  []Entry
}

type Entry struct {
	ID           string
	ProviderID   string
	ProviderName string
	Model        string
	Weight       int
	Status       Status
}

type EntryInput struct {
	ProviderID string
	Model      string
	Weight     int
}

type TierInput struct {
	Entries []EntryInput
}

type CreateInput struct {
	Name          string
	Kind          Kind
	Strategy      Strategy
	CacheAffinity bool
	Tiers         []TierInput
}

// UpdateInput has no name: names are immutable.
type UpdateInput struct {
	Kind          Kind
	Strategy      Strategy
	CacheAffinity bool
	Tiers         []TierInput
}

// UserError is safe to show to the owner (validation, not found, conflicts).
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

const providerKindOpenAICompatible = "openai_compatible"

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

func userError(msg string) error { return &UserError{Msg: msg} }

type Service struct {
	store     *store
	providers ProviderDirectory
}

func New(d Deps) *Service {
	return &Service{store: &store{db: d.DB}, providers: d.Providers}
}

// List returns models by name with entry status computed against the current providers.
func (s *Service) List(ctx context.Context) ([]Model, error) {
	models, err := s.store.list(ctx)
	if err != nil {
		return nil, err
	}
	return s.present(ctx, models)
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Model, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !namePattern.MatchString(in.Name) {
		return Model{}, userError("name must be 1-64 characters: letters, digits, '.', '_', ':' or '-', starting with a letter or digit")
	}
	tiers, err := s.prepare(ctx, in.Kind, in.Strategy, in.Tiers)
	if err != nil {
		return Model{}, err
	}
	in.Tiers = tiers
	m, err := s.store.create(ctx, in)
	if err != nil {
		return Model{}, err
	}
	return s.presentOne(ctx, m)
}

// Update replaces kind, strategy, cache affinity and the whole tier tree atomically.
func (s *Service) Update(ctx context.Context, id string, in UpdateInput) (Model, error) {
	tiers, err := s.prepare(ctx, in.Kind, in.Strategy, in.Tiers)
	if err != nil {
		return Model{}, err
	}
	in.Tiers = tiers
	m, err := s.store.update(ctx, id, in)
	if err != nil {
		return Model{}, err
	}
	return s.presentOne(ctx, m)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.delete(ctx, id)
}

// prepare validates settings, tier structure and providers; it returns the tiers normalized.
func (s *Service) prepare(ctx context.Context, kind Kind, strategy Strategy, tiers []TierInput) ([]TierInput, error) {
	if !slices.Contains([]Kind{KindChat, KindTranscription}, kind) {
		return nil, userError("unknown kind")
	}
	if !slices.Contains([]Strategy{StrategyRoundRobin, StrategyWeightedRoundRobin, StrategyPriorityOverflow, StrategyLeastInFlight}, strategy) {
		return nil, userError("unknown strategy")
	}
	tiers, err := normalizeTiers(tiers)
	if err != nil {
		return nil, err
	}
	return tiers, s.checkProviders(ctx, kind, tiers)
}

func normalizeTiers(tiers []TierInput) ([]TierInput, error) {
	if len(tiers) == 0 {
		return nil, userError("at least one tier is required")
	}
	out := make([]TierInput, len(tiers))
	for i, t := range tiers {
		entries, err := normalizeEntries(i+1, t.Entries)
		if err != nil {
			return nil, err
		}
		out[i] = TierInput{Entries: entries}
	}
	return out, nil
}

func normalizeEntries(tierNo int, in []EntryInput) ([]EntryInput, error) {
	if len(in) == 0 {
		return nil, userError(fmt.Sprintf("tier %d needs at least one entry", tierNo))
	}
	out := make([]EntryInput, len(in))
	seen := map[[2]string]bool{}
	for i, e := range in {
		e.ProviderID, e.Model = strings.TrimSpace(e.ProviderID), strings.TrimSpace(e.Model)
		switch {
		case e.ProviderID == "":
			return nil, userError(fmt.Sprintf("tier %d: provider is required", tierNo))
		case e.Model == "":
			return nil, userError(fmt.Sprintf("tier %d: model must not be blank", tierNo))
		case e.Weight < 1:
			return nil, userError(fmt.Sprintf("tier %d: weight must be at least 1", tierNo))
		case seen[[2]string{e.ProviderID, e.Model}]:
			return nil, userError(fmt.Sprintf("tier %d: duplicate provider and model %q", tierNo, e.Model))
		}
		seen[[2]string{e.ProviderID, e.Model}] = true
		out[i] = e
	}
	return out, nil
}

// checkProviders requires every entry's provider to exist, be enabled and fit the kind.
func (s *Service) checkProviders(ctx context.Context, kind Kind, tiers []TierInput) error {
	var ids []string
	for _, t := range tiers {
		for _, e := range t.Entries {
			ids = append(ids, e.ProviderID)
		}
	}
	infos, err := s.providers.ProviderInfo(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		info, found := infos[id]
		if msg := saveProblem(statusOf(kind, info, found), info); msg != "" {
			return userError(msg)
		}
	}
	return nil
}

func statusOf(kind Kind, info ProviderInfo, found bool) Status {
	switch {
	case !found:
		return StatusProviderMissing
	case !info.Enabled:
		return StatusProviderDisabled
	case kind == KindTranscription && info.Kind != providerKindOpenAICompatible:
		return StatusProviderIncompatible
	}
	return StatusOK
}

func saveProblem(status Status, info ProviderInfo) string {
	switch status {
	case StatusProviderMissing:
		return "provider not found"
	case StatusProviderDisabled:
		return fmt.Sprintf("provider %q is disabled", info.Name)
	case StatusProviderIncompatible:
		return fmt.Sprintf("provider %q is %s: audio transcription needs an OpenAI-compatible provider", info.Name, info.Kind)
	}
	return ""
}

func (s *Service) presentOne(ctx context.Context, m Model) (Model, error) {
	out, err := s.present(ctx, []Model{m})
	if err != nil {
		return Model{}, err
	}
	return out[0], nil
}

// present fills entry provider names and statuses with one directory call.
func (s *Service) present(ctx context.Context, models []Model) ([]Model, error) {
	var ids []string
	for _, m := range models {
		for _, t := range m.Tiers {
			for _, e := range t.Entries {
				ids = append(ids, e.ProviderID)
			}
		}
	}
	infos, err := s.providers.ProviderInfo(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		for _, t := range m.Tiers {
			for i := range t.Entries {
				e := &t.Entries[i]
				info, found := infos[e.ProviderID]
				e.ProviderName, e.Status = info.Name, statusOf(m.Kind, info, found)
			}
			slices.SortFunc(t.Entries, compareEntries)
		}
	}
	return models, nil
}

// compareEntries orders by provider name then model: order inside a tier carries no meaning.
func compareEntries(a, b Entry) int {
	if c := strings.Compare(a.ProviderName, b.ProviderName); c != 0 {
		return c
	}
	return strings.Compare(a.Model, b.Model)
}
