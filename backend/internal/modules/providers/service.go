// Package providers owns upstream AI providers and their encrypted API keys.
package providers

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
)

type Kind string

const (
	KindOpenAICompatible   Kind = "openai_compatible"
	KindAnthropic          Kind = "anthropic"
	KindClaudeSubscription Kind = "claude_subscription"
)

type Rotation string

const (
	RotationRoundRobin         Rotation = "round_robin"
	RotationWeightedRoundRobin Rotation = "weighted_round_robin"
	RotationPriorityOverflow   Rotation = "priority_overflow"
	RotationLeastInFlight      Rotation = "least_in_flight"
)

const (
	minMaskedKeyLen = 12
	maskPrefix      = "••••"
)

// Cipher seals and opens secrets; defined here so the module does not depend on a concrete implementation.
type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(sealed []byte) ([]byte, error)
}

type Deps struct {
	DB     *ent.Client
	Cipher Cipher
}

// Provider never carries a secret; Capacity is the sum of max in flight over enabled keys.
type Provider struct {
	ID                   string
	Name                 string
	Kind                 Kind
	BaseURL              string
	Enabled              bool
	Rotation             Rotation
	CacheAffinitySeconds int
	Capacity             int
	Keys                 []Key
}

type Key struct {
	ID          string
	Name        string
	Enabled     bool
	MaxInFlight int
	Weight      int
	Priority    int
	MaskedKey   string
}

// ProviderInput is used for create (Name, Kind, BaseURL required) and update (nil fields stay unchanged).
type ProviderInput struct {
	Name                 *string
	Kind                 *Kind
	BaseURL              *string
	Enabled              *bool
	Rotation             *Rotation
	CacheAffinitySeconds *int
}

// KeyInput is used for add (Name, Secret, MaxInFlight required) and update (nil fields stay unchanged).
type KeyInput struct {
	Name        *string
	Secret      *string
	Enabled     *bool
	MaxInFlight *int
	Weight      *int
	Priority    *int
}

// UserError is safe to show to the owner (validation, not found, conflicts).
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userError(msg string) error { return &UserError{Msg: msg} }

type Service struct {
	store  *store
	cipher Cipher
}

func New(d Deps) *Service {
	return &Service{store: &store{db: d.DB}, cipher: d.Cipher}
}

func (s *Service) ListProviders(ctx context.Context) ([]Provider, error) {
	return s.store.list(ctx)
}

func (s *Service) CreateProvider(ctx context.Context, in ProviderInput) (Provider, error) {
	in = trimProvider(in)
	if err := validateProviderCreate(in); err != nil {
		return Provider{}, err
	}
	return s.store.create(ctx, in)
}

func (s *Service) UpdateProvider(ctx context.Context, id string, in ProviderInput) (Provider, error) {
	in = trimProvider(in)
	if err := validateProvider(in); err != nil {
		return Provider{}, err
	}
	return s.store.update(ctx, id, in)
}

func (s *Service) DeleteProvider(ctx context.Context, id string) error {
	return s.store.delete(ctx, id)
}

func (s *Service) AddKey(ctx context.Context, providerID string, in KeyInput) (Key, error) {
	in = trimKey(in)
	if err := validateKeyCreate(in); err != nil {
		return Key{}, err
	}
	sealed, err := s.seal(*in.Secret)
	if err != nil {
		return Key{}, err
	}
	return s.store.addKey(ctx, providerID, in, sealed)
}

func (s *Service) UpdateKey(ctx context.Context, id string, in KeyInput) (Key, error) {
	in = trimKey(in)
	if err := validateKey(in); err != nil {
		return Key{}, err
	}
	var sealed *sealedSecret
	if in.Secret != nil {
		var err error
		if sealed, err = s.seal(*in.Secret); err != nil {
			return Key{}, err
		}
	}
	return s.store.updateKey(ctx, id, in, sealed)
}

func (s *Service) seal(secret string) (*sealedSecret, error) {
	ciphertext, err := s.cipher.Encrypt([]byte(secret))
	if err != nil {
		return nil, err
	}
	return &sealedSecret{ciphertext: ciphertext, last4: last4Of(secret)}, nil
}

func (s *Service) DeleteKey(ctx context.Context, id string) error {
	return s.store.deleteKey(ctx, id)
}

// Reveal decrypts a key for upstream use; never expose the result through the API.
func (s *Service) Reveal(ctx context.Context, keyID string) (string, error) {
	sealed, err := s.store.ciphertext(ctx, keyID)
	if err != nil {
		return "", err
	}
	plain, err := s.cipher.Decrypt(sealed)
	return string(plain), err
}

func validateProviderCreate(in ProviderInput) error {
	switch {
	case in.Name == nil:
		return userError("name is required")
	case in.Kind == nil:
		return userError("kind is required")
	case in.BaseURL == nil:
		return userError("base URL is required")
	}
	return validateProvider(in)
}

func validateProvider(in ProviderInput) error {
	if in.Name != nil && *in.Name == "" {
		return userError("name is required")
	}
	if in.Kind != nil && !slices.Contains([]Kind{KindOpenAICompatible, KindAnthropic, KindClaudeSubscription}, *in.Kind) {
		return userError("unknown kind")
	}
	if in.BaseURL != nil && !validBaseURL(*in.BaseURL) {
		return userError("base URL must be an http(s) URL with a host and no credentials, query or fragment")
	}
	if in.Rotation != nil && !slices.Contains([]Rotation{RotationRoundRobin, RotationWeightedRoundRobin, RotationPriorityOverflow, RotationLeastInFlight}, *in.Rotation) {
		return userError("unknown rotation")
	}
	if in.CacheAffinitySeconds != nil && *in.CacheAffinitySeconds < 0 {
		return userError("cache affinity seconds must be 0 or more")
	}
	return nil
}

func validBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func trim(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.TrimSpace(*p)
	return &t
}

func trimProvider(in ProviderInput) ProviderInput {
	in.Name, in.BaseURL = trim(in.Name), trim(in.BaseURL)
	return in
}

func trimKey(in KeyInput) KeyInput {
	in.Name, in.Secret = trim(in.Name), trim(in.Secret)
	return in
}

func validateKeyCreate(in KeyInput) error {
	switch {
	case in.Name == nil:
		return userError("name is required")
	case in.Secret == nil:
		return userError("secret is required")
	case in.MaxInFlight == nil:
		return userError("max in flight is required")
	}
	return validateKey(in)
}

func validateKey(in KeyInput) error {
	if in.Name != nil && *in.Name == "" {
		return userError("name is required")
	}
	if in.Secret != nil && *in.Secret == "" {
		return userError("secret is required")
	}
	if in.MaxInFlight != nil && *in.MaxInFlight < 1 {
		return userError("max in flight must be at least 1")
	}
	if in.Weight != nil && *in.Weight < 1 {
		return userError("weight must be at least 1")
	}
	return nil
}

func maskedKey(last4 string) string { return maskPrefix + last4 }

// last4Of keeps the tail only for secrets long enough that the tail reveals little.
func last4Of(secret string) string {
	if len(secret) < minMaskedKeyLen {
		return ""
	}
	return secret[len(secret)-4:]
}

// ByIDs is the batch lookup for other modules; unknown or malformed ids are ignored.
func (s *Service) ByIDs(ctx context.Context, ids []string) (map[string]Provider, error) {
	return s.store.byIDs(ctx, ids)
}
