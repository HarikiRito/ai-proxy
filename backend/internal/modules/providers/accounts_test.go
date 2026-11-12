package providers

import (
	"context"
	"strings"
	"testing"
	"time"
)

const token = "sk-ant-oat01-DISTINCTIVE-token-abcdef123456"

func subscription(t *testing.T, s *Service) Provider {
	t.Helper()
	in := validProvider()
	in.Name, in.Kind, in.BaseURL = ptr("sub"), ptr(KindClaudeSubscription), ptr("http://sidecar:3000")
	return mustCreate(t, s, in)
}

func account(name string, mode AuthMode, secret string) KeyInput {
	in := validKey(name, secret)
	in.AuthMode = &mode
	return in
}

func TestSubscriptionAccountModes(t *testing.T) {
	s, _ := newService(t)
	p := subscription(t, s)
	before := time.Now()

	tok := mustAddKey(t, s, p.ID, account("tok", AuthSetupToken, token))
	if tok.AuthMode != AuthSetupToken || tok.MaskedKey == token || strings.Contains(tok.MaskedKey, "DISTINCTIVE-token-abcdef12") {
		t.Fatalf("token key: %+v", tok)
	}
	if tok.TokenCreatedAt == nil || tok.TokenCreatedAt.Before(before.Add(-time.Second)) || !tok.TokenExpiresAt.Equal(tok.TokenCreatedAt.Add(365*24*time.Hour)) {
		t.Fatalf("dates: %+v", tok)
	}

	dir := mustAddKey(t, s, p.ID, account("dir", AuthLoginDir, "acct-1"))
	if dir.MaskedKey != "acct-1" || dir.TokenCreatedAt != nil || dir.TokenExpiresAt != nil {
		t.Fatalf("dir key: %+v", dir)
	}
	if got, _ := s.Reveal(context.Background(), dir.ID); got != "acct-1" {
		t.Fatalf("reveal %q", got)
	}
}

func TestAccountValidation(t *testing.T) {
	s, _ := newService(t)
	sub := subscription(t, s)
	api := mustCreate(t, s, validProvider())
	future := time.Now().Add(time.Hour)
	withDate := account("k", AuthSetupToken, token)
	withDate.TokenCreatedAt = &future
	noMode := validKey("k", token)
	apiMode := account("k", AuthAPIKey, token)
	dirOnAPI := account("k", AuthLoginDir, "acct")
	cases := map[string]struct {
		provider string
		in       KeyInput
		want     string
	}{
		"subscription needs a mode":    {sub.ID, noMode, "auth mode is required"},
		"subscription rejects api_key": {sub.ID, apiMode, "setup_token or login_dir"},
		"api provider rejects mode":    {api.ID, dirOnAPI, "only for claude subscription"},
		"bad dir name":                 {sub.ID, account("k", AuthLoginDir, "Bad/Dir"), "account directory"},
		"dir name too long":            {sub.ID, account("k", AuthLoginDir, strings.Repeat("a", 65)), "account directory"},
		"short token":                  {sub.ID, account("k", AuthSetupToken, "short"), "setup token looks invalid"},
		"token with newline":           {sub.ID, account("k", AuthSetupToken, token+"\nx"), "setup token looks invalid"},
		"future created date":          {sub.ID, withDate, "future"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := s.AddKey(context.Background(), tc.provider, tc.in)
			requireUserError(t, err, tc.want)
		})
	}
}

func TestAPIProviderKeysStayAPIKey(t *testing.T) {
	s, _ := newService(t)
	k := mustAddKey(t, s, mustCreate(t, s, validProvider()).ID, validKey("k", "sk-aaaaaaaaaaaa"))
	if k.AuthMode != AuthAPIKey || k.TokenCreatedAt != nil || k.TokenExpiresAt != nil {
		t.Fatalf("%+v", k)
	}
}

func TestUpdateAccountSecretAndDate(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	p := subscription(t, s)
	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
	in := account("tok", AuthSetupToken, token)
	in.TokenCreatedAt = &old
	k := mustAddKey(t, s, p.ID, in)
	if !k.TokenCreatedAt.Equal(old) {
		t.Fatalf("explicit date ignored: %+v", k)
	}

	_, err := s.UpdateKey(ctx, k.ID, KeyInput{Secret: ptr("")})
	requireUserError(t, err, "secret is required")
	k2, err := s.UpdateKey(ctx, k.ID, KeyInput{Name: ptr("renamed")})
	if err != nil || !k2.TokenCreatedAt.Equal(old) {
		t.Fatalf("blank secret must keep the date: %+v %v", k2, err)
	}
	if got, _ := s.Reveal(ctx, k.ID); got != token {
		t.Fatal("blank secret must keep the secret")
	}
	k2, err = s.UpdateKey(ctx, k.ID, KeyInput{AuthMode: ptr(AuthSetupToken), Enabled: ptr(false)})
	if err != nil || !k2.TokenCreatedAt.Equal(old) {
		t.Fatalf("restating the same mode must keep the date: %+v %v", k2, err)
	}

	k3, err := s.UpdateKey(ctx, k.ID, KeyInput{Secret: ptr(token + "-2")})
	if err != nil || time.Since(*k3.TokenCreatedAt) > time.Minute {
		t.Fatalf("new secret must reset the date to now: %+v %v", k3, err)
	}

	explicit := old.Add(24 * time.Hour)
	k4, err := s.UpdateKey(ctx, k.ID, KeyInput{Secret: ptr(token + "-3"), TokenCreatedAt: &explicit})
	if err != nil || !k4.TokenCreatedAt.Equal(explicit) {
		t.Fatalf("explicit date ignored: %+v %v", k4, err)
	}

	_, err = s.UpdateKey(ctx, k.ID, KeyInput{AuthMode: ptr(AuthLoginDir)})
	requireUserError(t, err, "secret is required when changing")
	k5, err := s.UpdateKey(ctx, k.ID, KeyInput{AuthMode: ptr(AuthLoginDir), Secret: ptr("acct")})
	if err != nil || k5.AuthMode != AuthLoginDir || k5.TokenCreatedAt != nil || k5.MaskedKey != "acct" {
		t.Fatalf("%+v %v", k5, err)
	}
}

func TestKindSwitchWithKeysRejectedBothWays(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	p := subscription(t, s)
	mustAddKey(t, s, p.ID, account("dir", AuthLoginDir, "acct"))
	_, err := s.UpdateProvider(ctx, p.ID, ProviderInput{Kind: ptr(KindOpenAICompatible)})
	requireUserError(t, err, "remove its keys first")
}
