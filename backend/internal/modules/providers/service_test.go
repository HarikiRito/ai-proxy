package providers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HarikiRito/ai-proxy/backend/internal/platform/crypto"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

func ptr[T any](v T) *T { return &v }

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
	cipher, err := crypto.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return New(Deps{DB: client, Cipher: cipher}), dir
}

func validProvider() ProviderInput {
	return ProviderInput{Name: ptr("openai"), Kind: ptr(KindOpenAICompatible), BaseURL: ptr("https://api.openai.com/v1")}
}

func validKey(name, secret string) KeyInput {
	return KeyInput{Name: ptr(name), Secret: ptr(secret), MaxInFlight: ptr(4)}
}

func mustCreate(t *testing.T, s *Service, in ProviderInput) Provider {
	t.Helper()
	p, err := s.CreateProvider(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustAddKey(t *testing.T, s *Service, providerID string, in KeyInput) Key {
	t.Helper()
	k, err := s.AddKey(context.Background(), providerID, in)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func requireUserError(t *testing.T, err error, contains string) {
	t.Helper()
	var ue *UserError
	if !errors.As(err, &ue) || !strings.Contains(ue.Msg, contains) {
		t.Fatalf("want user error containing %q, got %v", contains, err)
	}
}

func TestCreateProviderAppliesDefaults(t *testing.T) {
	s, _ := newService(t)
	p := mustCreate(t, s, validProvider())
	if !p.Enabled || p.Rotation != RotationRoundRobin || p.CacheAffinitySeconds != 600 || p.Capacity != 0 || len(p.Keys) != 0 {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}

func TestProviderValidation(t *testing.T) {
	cases := map[string]struct {
		mutate func(*ProviderInput)
		want   string
	}{
		"empty name":      {func(in *ProviderInput) { in.Name = ptr("") }, "name"},
		"missing name":    {func(in *ProviderInput) { in.Name = nil }, "name"},
		"bad kind":        {func(in *ProviderInput) { in.Kind = ptr(Kind("nope")) }, "kind"},
		"missing kind":    {func(in *ProviderInput) { in.Kind = nil }, "kind"},
		"no scheme":       {func(in *ProviderInput) { in.BaseURL = ptr("api.openai.com") }, "base URL"},
		"ftp scheme":      {func(in *ProviderInput) { in.BaseURL = ptr("ftp://x.test") }, "base URL"},
		"no host":         {func(in *ProviderInput) { in.BaseURL = ptr("https://") }, "base URL"},
		"bad rotation":    {func(in *ProviderInput) { in.Rotation = ptr(Rotation("nope")) }, "rotation"},
		"negative cache":  {func(in *ProviderInput) { in.CacheAffinitySeconds = ptr(-1) }, "cache"},
		"missing baseURL": {func(in *ProviderInput) { in.BaseURL = nil }, "base URL"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := newService(t)
			in := validProvider()
			tc.mutate(&in)
			_, err := s.CreateProvider(context.Background(), in)
			requireUserError(t, err, tc.want)
		})
	}
}

func TestDuplicateProviderNameIsUserError(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, validProvider())
	_, err := s.CreateProvider(context.Background(), validProvider())
	requireUserError(t, err, "already exists")
}

func TestUpdateProviderChangesOnlyGivenFields(t *testing.T) {
	s, _ := newService(t)
	p := mustCreate(t, s, validProvider())
	got, err := s.UpdateProvider(context.Background(), p.ID, ProviderInput{Enabled: ptr(false), Rotation: ptr(RotationLeastInFlight)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Rotation != RotationLeastInFlight || got.Name != "openai" || got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("unexpected: %+v", got)
	}
	_, err = s.UpdateProvider(context.Background(), p.ID, ProviderInput{BaseURL: ptr("nope")})
	requireUserError(t, err, "base URL")
}

func TestUnknownIDIsNotFound(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	const missing = "00000000-0000-0000-0000-000000000000"
	for name, err := range map[string]error{
		"update provider": second(s.UpdateProvider(ctx, missing, ProviderInput{})),
		"delete provider": s.DeleteProvider(ctx, missing),
		"add key":         second(s.AddKey(ctx, missing, validKey("k", "sk-secret-value"))),
		"update key":      second(s.UpdateKey(ctx, missing, KeyInput{})),
		"delete key":      s.DeleteKey(ctx, missing),
		"malformed id":    s.DeleteKey(ctx, "not-a-uuid"),
	} {
		t.Run(name, func(t *testing.T) { requireUserError(t, err, "not found") })
	}
}

func second[T any](_ T, err error) error { return err }

func TestKeyValidation(t *testing.T) {
	cases := map[string]struct {
		mutate func(*KeyInput)
		want   string
	}{
		"empty name":     {func(in *KeyInput) { in.Name = ptr("") }, "name"},
		"empty secret":   {func(in *KeyInput) { in.Secret = ptr("") }, "secret"},
		"missing secret": {func(in *KeyInput) { in.Secret = nil }, "secret"},
		"zero in flight": {func(in *KeyInput) { in.MaxInFlight = ptr(0) }, "max in flight"},
		"no in flight":   {func(in *KeyInput) { in.MaxInFlight = nil }, "max in flight"},
		"zero weight":    {func(in *KeyInput) { in.Weight = ptr(0) }, "weight"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := newService(t)
			p := mustCreate(t, s, validProvider())
			in := validKey("k", "sk-secret-value")
			tc.mutate(&in)
			_, err := s.AddKey(context.Background(), p.ID, in)
			requireUserError(t, err, tc.want)
		})
	}
}

func TestClaudeSubscriptionRejectsKeys(t *testing.T) {
	s, _ := newService(t)
	in := validProvider()
	in.Kind = ptr(KindClaudeSubscription)
	p := mustCreate(t, s, in)
	_, err := s.AddKey(context.Background(), p.ID, validKey("k", "sk-secret-value"))
	requireUserError(t, err, "subscription")
}

func TestMaskedKeyShowsLast4OnlyForLongSecrets(t *testing.T) {
	s, _ := newService(t)
	p := mustCreate(t, s, validProvider())
	long := mustAddKey(t, s, p.ID, validKey("long", "sk-abcdefgh1234"))
	short := mustAddKey(t, s, p.ID, validKey("short", "abcdef12345"))
	if long.MaskedKey != "••••1234" {
		t.Errorf("long: %q", long.MaskedKey)
	}
	if short.MaskedKey != "••••" {
		t.Errorf("short (11 chars): %q", short.MaskedKey)
	}
}

func TestCapacitySumsEnabledKeysOnly(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	p := mustCreate(t, s, validProvider())
	mustAddKey(t, s, p.ID, validKey("a", "sk-aaaaaaaaaaaa"))
	off := mustAddKey(t, s, p.ID, KeyInput{Name: ptr("b"), Secret: ptr("sk-bbbbbbbbbbbb"), MaxInFlight: ptr(10), Enabled: ptr(false)})
	if off.Enabled {
		t.Fatal("key should be disabled")
	}
	list, err := s.ListProviders(ctx)
	if err != nil || len(list) != 1 || list[0].Capacity != 4 || len(list[0].Keys) != 2 {
		t.Fatalf("got %+v, %v", list, err)
	}
	if _, err := s.UpdateKey(ctx, off.ID, KeyInput{Enabled: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListProviders(ctx)
	if list[0].Capacity != 14 {
		t.Fatalf("capacity %d", list[0].Capacity)
	}
}

func TestDuplicateKeyNamePerProvider(t *testing.T) {
	s, _ := newService(t)
	p := mustCreate(t, s, validProvider())
	mustAddKey(t, s, p.ID, validKey("k", "sk-aaaaaaaaaaaa"))
	_, err := s.AddKey(context.Background(), p.ID, validKey("k", "sk-bbbbbbbbbbbb"))
	requireUserError(t, err, "already exists")

	other := validProvider()
	other.Name = ptr("other")
	p2 := mustCreate(t, s, other)
	mustAddKey(t, s, p2.ID, validKey("k", "sk-cccccccccccc"))
}

func TestUpdateKeyReplacesSecret(t *testing.T) {
	s, _ := newService(t)
	p := mustCreate(t, s, validProvider())
	k := mustAddKey(t, s, p.ID, validKey("k", "sk-aaaaaaaa1111"))
	got, err := s.UpdateKey(context.Background(), k.ID, KeyInput{Secret: ptr("sk-bbbbbbbb2222"), MaxInFlight: ptr(9)})
	if err != nil || got.MaskedKey != "••••2222" || got.MaxInFlight != 9 || got.Name != "k" {
		t.Fatalf("got %+v, %v", got, err)
	}
	plain, err := s.Reveal(context.Background(), k.ID)
	if err != nil || plain != "sk-bbbbbbbb2222" {
		t.Fatalf("decrypted %q, %v", plain, err)
	}
}

func TestDeleteProviderCascadesKeys(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	p := mustCreate(t, s, validProvider())
	k := mustAddKey(t, s, p.ID, validKey("k", "sk-aaaaaaaaaaaa"))
	if err := s.DeleteProvider(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	requireUserError(t, s.DeleteKey(ctx, k.ID), "not found")
}

func TestDeleteKey(t *testing.T) {
	s, _ := newService(t)
	p := mustCreate(t, s, validProvider())
	k := mustAddKey(t, s, p.ID, validKey("k", "sk-aaaaaaaaaaaa"))
	if err := s.DeleteKey(context.Background(), k.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListProviders(context.Background())
	if len(list[0].Keys) != 0 {
		t.Fatalf("key not deleted: %+v", list[0].Keys)
	}
}

func TestPlaintextSecretNeverReachesDisk(t *testing.T) {
	s, dir := newService(t)
	const secret = "sk-DISTINCTIVE-plaintext-9f3a7c1e"
	p := mustCreate(t, s, validProvider())
	mustAddKey(t, s, p.ID, validKey("k", secret))

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no db files: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("%s contains the plaintext secret", f)
		}
		if bytes.Contains(data, []byte(secret[len(secret)-8:])) {
			t.Errorf("%s contains more than the last 4 chars of the secret", f)
		}
	}
}

func TestUpdateToClaudeSubscriptionRejectedWhileKeysExist(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	in := validProvider()
	in.Kind = ptr(KindAnthropic)
	p := mustCreate(t, s, in)
	mustAddKey(t, s, p.ID, validKey("k", "sk-aaaaaaaaaaaa"))

	_, err := s.UpdateProvider(ctx, p.ID, ProviderInput{Kind: ptr(KindClaudeSubscription)})
	requireUserError(t, err, "remove its keys first")
	list, _ := s.ListProviders(ctx)
	if list[0].Kind != KindAnthropic || len(list[0].Keys) != 1 {
		t.Fatalf("provider changed: %+v", list[0])
	}
}

func TestBaseURLRejectsUserinfoQueryAndFragment(t *testing.T) {
	for _, raw := range []string{"https://user:pass@host.test/v1", "https://host.test/v1?key=x", "https://host.test/v1#f"} {
		s, _ := newService(t)
		in := validProvider()
		in.BaseURL = ptr(raw)
		_, err := s.CreateProvider(context.Background(), in)
		requireUserError(t, err, "base URL")
	}
}

func TestPresetBaseURLsAreAccepted(t *testing.T) {
	for _, raw := range []string{
		"https://api.openai.com/v1", "https://api.anthropic.com", "https://api.groq.com/openai/v1",
		"https://api.together.ai/v1", "https://api.fireworks.ai/inference/v1", "https://api.deepseek.com",
		"https://generativelanguage.googleapis.com/v1beta/openai/", "https://openrouter.ai/api/v1",
	} {
		if !validBaseURL(raw) {
			t.Errorf("%s rejected", raw)
		}
	}
}

func TestInputsAreTrimmed(t *testing.T) {
	s, _ := newService(t)
	in := validProvider()
	in.Name = ptr("  openai \n")
	in.BaseURL = ptr(" https://api.openai.com/v1\n")
	p := mustCreate(t, s, in)
	if p.Name != "openai" || p.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("not trimmed: %+v", p)
	}
	k := mustAddKey(t, s, p.ID, KeyInput{Name: ptr(" main "), Secret: ptr(" sk-abcdefgh1234\n"), MaxInFlight: ptr(1)})
	plain, err := s.Reveal(context.Background(), k.ID)
	if k.Name != "main" || k.MaskedKey != "••••1234" || err != nil || plain != "sk-abcdefgh1234" {
		t.Fatalf("key not trimmed: %+v %q %v", k, plain, err)
	}
}

func TestWhitespaceOnlyNameAndSecretRejected(t *testing.T) {
	s, _ := newService(t)
	in := validProvider()
	in.Name = ptr("   ")
	_, err := s.CreateProvider(context.Background(), in)
	requireUserError(t, err, "name")

	p := mustCreate(t, s, validProvider())
	_, err = s.AddKey(context.Background(), p.ID, validKey("   ", "sk-aaaaaaaaaaaa"))
	requireUserError(t, err, "name")
	_, err = s.AddKey(context.Background(), p.ID, validKey("k", " \n\t"))
	requireUserError(t, err, "secret")
}

func TestByIDsReturnsKnownProvidersAndIgnoresUnknown(t *testing.T) {
	s, _ := newService(t)
	a := mustCreate(t, s, validProvider())
	in := validProvider()
	in.Name = ptr("second")
	b := mustCreate(t, s, in)

	got, err := s.ByIDs(context.Background(), []string{a.ID, b.ID, "00000000-0000-0000-0000-000000000000", "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[a.ID].Name != "openai" || got[b.ID].Name != "second" {
		t.Fatalf("got %+v", got)
	}
	if empty, err := s.ByIDs(context.Background(), nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty: %v %v", empty, err)
	}
}
