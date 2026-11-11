package proxykeys

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

func newService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(dir, "app.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return New(Deps{DB: client}), dir
}

func mustCreate(t *testing.T, s *Service, name string, models ...string) (Key, string) {
	t.Helper()
	k, secret, err := s.Create(context.Background(), CreateInput{Name: name, AllowedModels: models})
	if err != nil {
		t.Fatal(err)
	}
	return k, secret
}

func TestGeneratedKeyFormat(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		key, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := strings.CutPrefix(key, Prefix)
		if !ok {
			t.Fatalf("missing prefix: %s", key)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("want 32 random bytes, got %d (%v)", len(decoded), err)
		}
		if seen[key] {
			t.Fatal("duplicate key")
		}
		seen[key] = true
	}
}

func TestSecretIsStoredAsSha256HashOnly(t *testing.T) {
	s, dir := newService(t)
	_, secret := mustCreate(t, s, "ci")

	hash := hashOf(secret)
	if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
		t.Fatalf("hash is not sha256 hex: %s", hash)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("plaintext key found in database file")
	}
	if wal, err := os.ReadFile(filepath.Join(dir, "app.db-wal")); err == nil && bytes.Contains(wal, []byte(secret)) {
		t.Fatal("plaintext key found in WAL")
	}
	if !bytes.Contains(raw, []byte(hash)) {
		if wal, _ := os.ReadFile(filepath.Join(dir, "app.db-wal")); !bytes.Contains(wal, []byte(hash)) {
			t.Fatal("hash not persisted")
		}
	}
}

func TestVerify(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	created, secret := mustCreate(t, s, "ci", "fast")

	got, err := s.Verify(ctx, secret)
	if err != nil || got.ID != created.ID || got.Name != "ci" {
		t.Fatalf("verify: %+v %v", got, err)
	}

	revoked, revokedSecret := mustCreate(t, s, "old")
	if _, err := s.Revoke(ctx, revoked.ID); err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{
		"empty":      "",
		"no prefix":  strings.TrimPrefix(secret, Prefix),
		"bare":       Prefix,
		"unknown":    Prefix + strings.Repeat("A", 43),
		"malformed":  Prefix + "!!!",
		"revoked":    revokedSecret,
		"whitespace": " " + secret,
	} {
		if _, err := s.Verify(ctx, token); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: want ErrInvalidKey, got %v", name, err)
		}
	}
}

func TestRevokeIsIdempotentAndKeepsFirstTimestamp(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	k, _ := mustCreate(t, s, "ci")

	first, err := s.Revoke(ctx, k.ID)
	if err != nil || first.RevokedAt == nil || first.Status != StatusRevoked {
		t.Fatalf("revoke: %+v %v", first, err)
	}
	time.Sleep(5 * time.Millisecond)
	second, err := s.Revoke(ctx, k.ID)
	if err != nil || second.RevokedAt == nil || !second.RevokedAt.Equal(*first.RevokedAt) {
		t.Fatalf("second revoke changed timestamp: %+v %v", second, err)
	}
}

func TestAllowedModelsAreNormalized(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()

	k, _ := mustCreate(t, s, "ci", " fast ", "smart", "fast")
	if !slices.Equal(k.AllowedModels, []string{"fast", "smart"}) {
		t.Fatalf("trim+dedupe: %v", k.AllowedModels)
	}
	if _, _, err := s.Create(ctx, CreateInput{Name: "bad", AllowedModels: []string{"fast", "  "}}); err == nil {
		t.Fatal("blank model must be rejected")
	}
	if _, err := s.SetAllowedModels(ctx, k.ID, []string{""}); err == nil {
		t.Fatal("blank model must be rejected on update")
	}

	updated, err := s.SetAllowedModels(ctx, k.ID, []string{"a", "b"})
	if err != nil || !slices.Equal(updated.AllowedModels, []string{"a", "b"}) {
		t.Fatalf("update: %+v %v", updated, err)
	}
	all, err := s.SetAllowedModels(ctx, k.ID, nil)
	if err != nil || all.AllowedModels == nil || len(all.AllowedModels) != 0 {
		t.Fatalf("clear: %+v %v", all, err)
	}
	if !all.AllowsModel("anything") {
		t.Error("empty list allows every model")
	}
	if !updated.AllowsModel("a") || updated.AllowsModel("c") {
		t.Error("allow-list not enforced")
	}
}

func TestTouchSetsLastUsedAt(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	k, _ := mustCreate(t, s, "ci")
	if k.LastUsedAt != nil {
		t.Fatal("new key must be unused")
	}
	if err := s.Touch(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 || list[0].LastUsedAt == nil {
		t.Fatalf("list after touch: %+v %v", list, err)
	}
}

func TestListNewestFirstAndStatus(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a, _ := mustCreate(t, s, "a")
	time.Sleep(5 * time.Millisecond)
	b, _ := mustCreate(t, s, "b")
	if _, err := s.Revoke(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID != b.ID {
		t.Fatalf("order: %+v %v", list, err)
	}
	if list[0].Status != StatusActive || list[1].Status != StatusRevoked {
		t.Fatalf("status: %+v", list)
	}
}

func TestUserErrors(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	mustCreate(t, s, "ci")

	cases := map[string]error{}
	_, _, cases["duplicate name"] = s.Create(ctx, CreateInput{Name: " ci "})
	_, _, cases["blank name"] = s.Create(ctx, CreateInput{Name: "  "})
	_, cases["revoke unknown"] = s.Revoke(ctx, "7b9d8f1e-0000-4000-8000-000000000000")
	_, cases["revoke bad id"] = s.Revoke(ctx, "nope")
	_, cases["models unknown"] = s.SetAllowedModels(ctx, "7b9d8f1e-0000-4000-8000-000000000000", nil)
	wantMsg := map[string]string{
		"duplicate name": "name already exists", "blank name": "name is required",
		"revoke unknown": "not found", "revoke bad id": "not found", "models unknown": "not found",
	}
	for name, err := range cases {
		var ue *UserError
		if !errors.As(err, &ue) || ue.Msg != wantMsg[name] {
			t.Errorf("%s: got %v", name, err)
		}
	}
}
