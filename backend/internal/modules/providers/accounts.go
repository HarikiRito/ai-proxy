package providers

import (
	"strings"
	"time"
	"unicode"
)

// keyAuth is the auth part of a key: how its secret is used and, for setup tokens, when it was issued.
type keyAuth struct {
	mode    AuthMode
	created *time.Time
}

// resolveAuth merges the input over the current auth (nil on add) and checks it against the provider kind.
func resolveAuth(kind Kind, cur *keyAuth, in KeyInput, now time.Time) (keyAuth, error) {
	out := keyAuth{mode: AuthAPIKey}
	switch {
	case in.AuthMode != nil:
		out.mode = *in.AuthMode
	case cur != nil:
		out = *cur
	case kind == KindClaudeSubscription:
		return keyAuth{}, userError("auth mode is required for claude subscription accounts")
	}
	if err := checkMode(kind, out.mode); err != nil {
		return keyAuth{}, err
	}
	if cur != nil && out.mode != cur.mode && in.Secret == nil {
		return keyAuth{}, userError("secret is required when changing the auth mode")
	}
	if in.Secret != nil {
		if err := checkSecret(out.mode, *in.Secret); err != nil {
			return keyAuth{}, err
		}
	}
	created, err := tokenCreated(out, in, now)
	out.created = created
	return out, err
}

func checkMode(kind Kind, mode AuthMode) error {
	if kind == KindClaudeSubscription {
		if mode != AuthSetupToken && mode != AuthLoginDir {
			return userError("claude subscription accounts use auth mode setup_token or login_dir")
		}
		return nil
	}
	if mode != AuthAPIKey {
		return userError("auth mode " + string(mode) + " is only for claude subscription providers")
	}
	return nil
}

func checkSecret(mode AuthMode, secret string) error {
	switch mode {
	case AuthLoginDir:
		if !loginDirName.MatchString(secret) {
			return userError("account directory must be 1 to 64 characters of a-z, 0-9, _ or -")
		}
	case AuthSetupToken:
		if len(secret) < minTokenLen || strings.ContainsFunc(secret, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return userError("setup token looks invalid")
		}
	}
	return nil
}

// tokenCreated is the issue date of a setup token: the given one, else now when the secret is new.
func tokenCreated(auth keyAuth, in KeyInput, now time.Time) (*time.Time, error) {
	if auth.mode != AuthSetupToken {
		if in.TokenCreatedAt != nil {
			return nil, userError("token created date applies to setup tokens only")
		}
		return nil, nil
	}
	switch {
	case in.TokenCreatedAt != nil && in.TokenCreatedAt.After(now):
		return nil, userError("token created date must not be in the future")
	case in.TokenCreatedAt != nil:
		return in.TokenCreatedAt, nil
	case in.Secret != nil || auth.created == nil:
		return &now, nil
	}
	return auth.created, nil
}
