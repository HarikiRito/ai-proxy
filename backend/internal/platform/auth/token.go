package auth

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/golang-jwt/jwt/v5"
)

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || scheme != "Bearer" || token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

// verify checks signature, issuer and time claims and returns the subject.
// Audience is deliberately not checked: the client-specific JWKS plus exact issuer bind the token.
func (a *Authenticator) verify(raw string) (string, error) {
	tok, err := jwt.Parse(raw, a.jwks.Keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(a.cfg.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(tokenLeeway),
		jwt.WithTimeFunc(a.cfg.Now),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenUnverifiable) && a.health.failing.Load() {
			return "", errUnavailable
		}
		return "", errUnauthorized
	}
	sub, _ := tok.Claims.GetSubject()
	if sub == "" || strings.Contains(sub, "/") {
		return "", errUnauthorized
	}
	return sub, nil
}

// healthTransport records whether the latest JWKS fetch succeeded, so a key lookup that failed
// because WorkOS is unreachable can be told apart from a token signed by an unknown key.
type healthTransport struct {
	base    http.RoundTripper
	failing atomic.Bool
}

func (h *healthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := h.base.RoundTrip(r)
	h.failing.Store(err != nil || resp.StatusCode != http.StatusOK)
	return resp, err
}
