// Package auth gates routes to the single owner: a WorkOS AuthKit access token (RS256, client JWKS)
// whose subject resolves, through the WorkOS Users API, to the verified owner email.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"golang.org/x/time/rate"
)

const (
	defaultJWKSBaseURL = "https://api.workos.com"
	defaultAPIBaseURL  = "https://api.workos.com"
	httpTimeout        = 5 * time.Second
	jwksRefresh        = time.Hour
	unknownKIDInterval = 30 * time.Second
	// also bounds the refresh request itself; shorter than the interval so limited lookups fail at once
	unknownKIDWait = 2 * time.Second
	tokenLeeway    = 5 * time.Second
)

type Config struct {
	ClientID   string
	APIKey     string
	Issuer     string
	OwnerEmail string
	// JWKSBaseURL and APIBaseURL default to WorkOS; tests point them at fakes.
	JWKSBaseURL string
	APIBaseURL  string
	Now         func() time.Time
}

type Authenticator struct {
	cfg    Config
	jwks   keyfunc.Keyfunc
	health *healthTransport
	users  *ownerLookup
}

func New(ctx context.Context, cfg Config) (*Authenticator, error) {
	if cfg.JWKSBaseURL == "" {
		cfg.JWKSBaseURL = defaultJWKSBaseURL
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = defaultAPIBaseURL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	health := &healthTransport{base: http.DefaultTransport}
	jwks, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{cfg.JWKSBaseURL + "/sso/jwks/" + cfg.ClientID}, keyfunc.Override{
		Client:            &http.Client{Transport: health, Timeout: httpTimeout},
		RefreshInterval:   jwksRefresh,
		RefreshUnknownKID: rate.NewLimiter(rate.Every(unknownKIDInterval), 1),
		RateLimitWaitMax:  unknownKIDWait,
	})
	if err != nil {
		return nil, err
	}
	return &Authenticator{cfg: cfg, jwks: jwks, health: health, users: newOwnerLookup(cfg)}, nil
}

var (
	errUnauthorized = errors.New("unauthorized")
	errForbidden    = errors.New("forbidden")
	errUnavailable  = errors.New("authentication unavailable")
)

type ownerKey struct{}

func WithOwnerEmail(ctx context.Context, email string) context.Context {
	return context.WithValue(ctx, ownerKey{}, email)
}

// OwnerEmail returns the verified owner email set by Middleware, or "" outside it.
func OwnerEmail(ctx context.Context) string {
	email, _ := ctx.Value(ownerKey{}).(string)
	return email
}

func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, err := a.authenticate(r)
		if err != nil {
			writeError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithOwnerEmail(r.Context(), email)))
	})
}

func (a *Authenticator) authenticate(r *http.Request) (string, error) {
	raw, ok := bearerToken(r)
	if !ok {
		return "", errUnauthorized
	}
	sub, err := a.verify(raw)
	if err != nil {
		return "", err
	}
	return a.users.owner(r.Context(), sub)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, errUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, errForbidden):
		status = http.StatusForbidden
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
