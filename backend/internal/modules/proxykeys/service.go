// Package proxykeys owns the credentials clients use to call the proxy. Only a sha256 hash is stored.
package proxykeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
)

// Prefix marks every proxy key; tokens without it are rejected before any lookup.
const Prefix = "sk-apx-"

const keyBytes = 32

type Status string

const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
)

// ErrInvalidKey is the single failure of Verify, so callers cannot tell unknown from revoked.
var ErrInvalidKey = errors.New("invalid proxy key")

type Deps struct {
	DB *ent.Client
}

// Key never carries the secret or its hash. An empty AllowedModels list means every model.
type Key struct {
	ID            string
	Name          string
	AllowedModels []string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
	Status        Status
}

func (k Key) AllowsModel(name string) bool {
	return len(k.AllowedModels) == 0 || slices.Contains(k.AllowedModels, name)
}

type CreateInput struct {
	Name          string
	AllowedModels []string
}

// UserError is safe to show to the owner (validation, not found, conflicts).
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userError(msg string) error { return &UserError{Msg: msg} }

type Service struct {
	store *store
}

func New(d Deps) *Service {
	return &Service{store: &store{db: d.DB}}
}

// Create returns the plaintext key exactly once; it is not recoverable afterwards.
func (s *Service) Create(ctx context.Context, in CreateInput) (Key, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Key{}, "", userError("name is required")
	}
	models, err := normalizeModels(in.AllowedModels)
	if err != nil {
		return Key{}, "", err
	}
	secret, err := generate()
	if err != nil {
		return Key{}, "", err
	}
	key, err := s.store.create(ctx, name, hashOf(secret), models)
	return key, secret, err
}

// List returns keys newest first.
func (s *Service) List(ctx context.Context) ([]Key, error) {
	return s.store.list(ctx)
}

// Revoke is idempotent: the first revocation time is kept.
func (s *Service) Revoke(ctx context.Context, id string) (Key, error) {
	return s.store.revoke(ctx, id)
}

// SetAllowedModels replaces the allow-list; empty means every model.
func (s *Service) SetAllowedModels(ctx context.Context, id string, models []string) (Key, error) {
	normalized, err := normalizeModels(models)
	if err != nil {
		return Key{}, err
	}
	return s.store.setAllowedModels(ctx, id, normalized)
}

// Verify checks a presented token; it is uncached so revocation applies immediately.
func (s *Service) Verify(ctx context.Context, token string) (Key, error) {
	if !wellFormed(token) {
		return Key{}, ErrInvalidKey
	}
	hash := hashOf(token)
	key, stored, err := s.store.byHash(ctx, hash)
	if err != nil {
		return Key{}, err
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(hash)) != 1 || key.Status == StatusRevoked {
		return Key{}, ErrInvalidKey
	}
	return key, nil
}

// Touch records a use of the key.
func (s *Service) Touch(ctx context.Context, id string) error {
	return s.store.touch(ctx, id)
}

func generate() (string, error) {
	b := make([]byte, keyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return Prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashOf(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func wellFormed(token string) bool {
	raw, ok := strings.CutPrefix(token, Prefix)
	if !ok {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	return err == nil && len(decoded) == keyBytes
}

// normalizeModels trims and dedupes preserving order; blank entries are rejected.
func normalizeModels(models []string) ([]string, error) {
	out := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			return nil, userError("model names must not be blank")
		}
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out, nil
}
