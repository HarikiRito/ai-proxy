package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/HarikiRito/ai-proxy/backend/internal/platform/auth"
)

const (
	clientID   = "client_test"
	issuer     = "https://issuer.test/"
	ownerEmail = "owner@example.com"
	apiKey     = "sk_test_key"
	ownerSub   = "user_owner"
)

type signer struct {
	kid string
	key *rsa.PrivateKey
}

func newSigner(t *testing.T, kid string) signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return signer{kid: kid, key: key}
}

func (s signer) jwk() map[string]string {
	return map[string]string{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": s.kid,
		"n": base64.RawURLEncoding.EncodeToString(s.key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.key.E)).Bytes()),
	}
}

func (s signer) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = s.kid
	raw, err := tok.SignedString(s.key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type jwksServer struct {
	*httptest.Server
	mu      sync.Mutex
	signers []signer
	status  int
	hits    atomic.Int32
}

func newJWKS(t *testing.T, signers ...signer) *jwksServer {
	t.Helper()
	s := &jwksServer{signers: signers, status: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Path != "/sso/jwks/"+clientID {
			http.NotFound(w, r)
			return
		}
		if s.status != http.StatusOK {
			w.WriteHeader(s.status)
			return
		}
		keys := []map[string]string{}
		for _, sg := range s.signers {
			keys = append(keys, sg.jwk())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) set(signers ...signer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signers = signers
}

type usersServer struct {
	*httptest.Server
	mu     sync.Mutex
	status int
	body   string
	hits   atomic.Int32
	auth   atomic.Value
}

func newUsers(t *testing.T) *usersServer {
	t.Helper()
	u := &usersServer{status: http.StatusOK, body: userBody(ownerEmail, true)}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		u.auth.Store(r.Header.Get("Authorization"))
		u.mu.Lock()
		defer u.mu.Unlock()
		if !strings.HasPrefix(r.URL.Path, "/user_management/users/") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(u.status)
		_, _ = w.Write([]byte(u.body))
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *usersServer) respond(status int, body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status, u.body = status, body
}

func userBody(email string, verified bool) string {
	b, _ := json.Marshal(map[string]any{"email": email, "email_verified": verified})
	return string(b)
}

type env struct {
	sg    signer
	jwks  *jwksServer
	users *usersServer
	mw    func(http.Handler) http.Handler
	now   *atomic.Int64
}

func (e *env) advance(d time.Duration) { e.now.Add(int64(d)) }

func newEnv(t *testing.T) *env {
	t.Helper()
	sg := newSigner(t, "kid1")
	e := &env{sg: sg, jwks: newJWKS(t, sg), users: newUsers(t), now: &atomic.Int64{}}
	e.now.Store(time.Now().UnixNano())
	return e.build(t)
}

func (e *env) build(t *testing.T) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a, err := auth.New(ctx, auth.Config{
		ClientID:    clientID,
		APIKey:      apiKey,
		Issuer:      issuer,
		OwnerEmail:  ownerEmail,
		JWKSBaseURL: e.jwks.URL,
		APIBaseURL:  e.users.URL,
		Now:         func() time.Time { return time.Unix(0, e.now.Load()) },
	})
	if err != nil {
		t.Fatal(err)
	}
	e.mw = a.Middleware
	return e
}

func (e *env) claims(mut func(jwt.MapClaims)) jwt.MapClaims {
	now := time.Unix(0, e.now.Load())
	c := jwt.MapClaims{
		"sub": ownerSub, "iss": issuer,
		"iat": now.Unix(), "nbf": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	if mut != nil {
		mut(c)
	}
	return c
}

func (e *env) do(authHeader string) *httptest.ResponseRecorder {
	var gotEmail string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEmail = auth.OwnerEmail(r.Context())
		_, _ = w.Write([]byte("ok:" + gotEmail))
	})
	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	e.mw(next).ServeHTTP(rec, req)
	return rec
}

func (e *env) bearer(t *testing.T, mut func(jwt.MapClaims)) *httptest.ResponseRecorder {
	t.Helper()
	return e.do("Bearer " + e.sg.sign(t, e.claims(mut)))
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), want)
	}
	if want >= 400 {
		var body map[string]string
		if json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["error"] == "" {
			t.Fatalf("want JSON error body, got %q", rec.Body.String())
		}
	}
}

func TestRejectsInvalidTokensWith401(t *testing.T) {
	e := newEnv(t)
	other := newSigner(t, "kid1")
	noneTok, err := jwt.NewWithClaims(jwt.SigningMethodNone, e.claims(nil)).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, e.claims(nil))
	hs.Header["kid"] = "kid1"
	hsTok, err := hs.SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	noExp := e.claims(func(c jwt.MapClaims) { delete(c, "exp") })

	tests := map[string]string{
		"no header":      "",
		"basic scheme":   "Basic abc",
		"empty bearer":   "Bearer ",
		"garbage":        "Bearer not.a.jwt",
		"expired":        "Bearer " + e.sg.sign(t, e.claims(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() })),
		"nbf in future":  "Bearer " + e.sg.sign(t, e.claims(func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() })),
		"missing exp":    "Bearer " + e.sg.sign(t, noExp),
		"wrong issuer":   "Bearer " + e.sg.sign(t, e.claims(func(c jwt.MapClaims) { c["iss"] = "https://evil.test/" })),
		"foreign key":    "Bearer " + other.sign(t, e.claims(nil)),
		"alg none":       "Bearer " + noneTok,
		"hs256":          "Bearer " + hsTok,
		"unknown kid":    "Bearer " + newSigner(t, "nope").sign(t, e.claims(nil)),
		"missing sub":    "Bearer " + e.sg.sign(t, e.claims(func(c jwt.MapClaims) { delete(c, "sub") })),
		"lowercase type": "bearer " + e.sg.sign(t, e.claims(nil)) + " extra",
	}
	for name, header := range tests {
		t.Run(name, func(t *testing.T) {
			wantStatus(t, e.do(header), http.StatusUnauthorized)
		})
	}
	if n := e.users.hits.Load(); n != 0 {
		t.Fatalf("users API called %d times for rejected tokens", n)
	}
}

func TestOwnerPassesAndEmailIsInContext(t *testing.T) {
	e := newEnv(t)
	rec := e.bearer(t, nil)
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.String() != "ok:"+ownerEmail {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := e.users.auth.Load(); got != "Bearer "+apiKey {
		t.Fatalf("users API auth = %v", got)
	}
}

func TestTokenWithoutNbfIsAccepted(t *testing.T) {
	e := newEnv(t)
	wantStatus(t, e.bearer(t, func(c jwt.MapClaims) { delete(c, "nbf") }), http.StatusOK)
}

func TestOwnerCheck(t *testing.T) {
	tests := map[string]struct {
		status int
		body   string
		want   int
	}{
		"email case-insensitive": {200, userBody("OWNER@Example.COM", true), 200},
		"wrapped user object":    {200, `{"user":{"email":"owner@example.com","email_verified":true}}`, 200},
		"wrapped unverified":     {200, `{"user":{"email":"owner@example.com","email_verified":false}}`, 403},
		"non owner":              {200, userBody("other@example.com", true), 403},
		"unverified email":       {200, userBody(ownerEmail, false), 403},
		"user not found":         {404, `{}`, 403},
		"api key rejected":       {401, `{}`, 503},
		"rate limited":           {429, `{}`, 503},
		"server error":           {500, `{}`, 503},
		"malformed body":         {200, `nope`, 503},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.users.respond(tt.status, tt.body)
			wantStatus(t, e.bearer(t, nil), tt.want)
		})
	}
}

func TestOwnerLookupNetworkFailureIs503(t *testing.T) {
	e := newEnv(t)
	e.users.Close()
	wantStatus(t, e.bearer(t, nil), http.StatusServiceUnavailable)
}

func TestOwnerLookupIsCachedPerSubForFiveMinutes(t *testing.T) {
	e := newEnv(t)
	wantStatus(t, e.bearer(t, nil), http.StatusOK)
	wantStatus(t, e.bearer(t, nil), http.StatusOK)
	if n := e.users.hits.Load(); n != 1 {
		t.Fatalf("users API hits = %d, want 1", n)
	}
	e.advance(4 * time.Minute)
	wantStatus(t, e.bearer(t, nil), http.StatusOK)
	if n := e.users.hits.Load(); n != 1 {
		t.Fatalf("hits before expiry = %d, want 1", n)
	}
	e.advance(2 * time.Minute)
	wantStatus(t, e.bearer(t, nil), http.StatusOK)
	if n := e.users.hits.Load(); n != 2 {
		t.Fatalf("hits after expiry = %d, want 2", n)
	}
}

func TestNonOwnerIsCachedToo(t *testing.T) {
	e := newEnv(t)
	e.users.respond(200, userBody("other@example.com", true))
	wantStatus(t, e.bearer(t, nil), http.StatusForbidden)
	wantStatus(t, e.bearer(t, nil), http.StatusForbidden)
	if n := e.users.hits.Load(); n != 1 {
		t.Fatalf("users API hits = %d, want 1", n)
	}
}

func TestInfraFailuresAreNotCached(t *testing.T) {
	e := newEnv(t)
	e.users.respond(500, `{}`)
	wantStatus(t, e.bearer(t, nil), http.StatusServiceUnavailable)
	e.users.respond(200, userBody(ownerEmail, true))
	wantStatus(t, e.bearer(t, nil), http.StatusOK)
}

func TestJWKSOutageIs503(t *testing.T) {
	e := newEnv(t)
	e.jwks.status = http.StatusInternalServerError
	e.build(t)
	wantStatus(t, e.bearer(t, nil), http.StatusServiceUnavailable)
}

func TestUnknownKIDRefreshesOnceThenAcceptsNewKey(t *testing.T) {
	e := newEnv(t)
	rotated := newSigner(t, "kid2")
	e.jwks.set(e.sg, rotated)
	before := e.jwks.hits.Load()

	rec := e.do("Bearer " + rotated.sign(t, e.claims(nil)))
	wantStatus(t, rec, http.StatusOK)
	if n := e.jwks.hits.Load() - before; n != 1 {
		t.Fatalf("refreshes = %d, want 1", n)
	}

	// further unknown kids are rate limited and answer 401 without hammering the JWKS endpoint
	before = e.jwks.hits.Load()
	wantStatus(t, e.do("Bearer "+newSigner(t, "kid3").sign(t, e.claims(nil))), http.StatusUnauthorized)
	wantStatus(t, e.do("Bearer "+newSigner(t, "kid4").sign(t, e.claims(nil))), http.StatusUnauthorized)
	if n := e.jwks.hits.Load() - before; n > 1 {
		t.Fatalf("refreshes for unknown kids = %d, want <= 1", n)
	}
}
