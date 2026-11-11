package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ownerCacheTTL = 5 * time.Minute

type cacheEntry struct {
	email   string
	isOwner bool
	expires time.Time
}

// ownerLookup resolves a token subject to the owner email via the WorkOS Users API,
// caching both owner and non-owner answers per subject.
type ownerLookup struct {
	cfg    Config
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cacheEntry
}

func newOwnerLookup(cfg Config) *ownerLookup {
	return &ownerLookup{cfg: cfg, client: &http.Client{Timeout: httpTimeout}, cache: map[string]cacheEntry{}}
}

func (o *ownerLookup) owner(ctx context.Context, sub string) (string, error) {
	entry, ok := o.cached(sub)
	if !ok {
		var err error
		if entry, err = o.fetch(ctx, sub); err != nil {
			return "", err
		}
		o.mu.Lock()
		o.cache[sub] = entry
		o.mu.Unlock()
	}
	if !entry.isOwner {
		return "", errForbidden
	}
	return entry.email, nil
}

func (o *ownerLookup) cached(sub string) (cacheEntry, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	entry, ok := o.cache[sub]
	if ok && !o.cfg.Now().Before(entry.expires) {
		delete(o.cache, sub)
		return cacheEntry{}, false
	}
	return entry, ok
}

type userFields struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// userResponse accepts both documented shapes: fields at top level or wrapped in "user".
type userResponse struct {
	userFields
	User *userFields `json:"user"`
}

func (o *ownerLookup) fetch(ctx context.Context, sub string) (cacheEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.cfg.APIBaseURL+"/user_management/users/", nil)
	if err != nil {
		return cacheEntry{}, errUnavailable
	}
	req.URL = req.URL.JoinPath(sub)
	req.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	resp, err := o.client.Do(req)
	if err != nil {
		return cacheEntry{}, errUnavailable
	}
	defer func() { _ = resp.Body.Close() }()

	expires := o.cfg.Now().Add(ownerCacheTTL)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return cacheEntry{expires: expires}, nil
	case resp.StatusCode != http.StatusOK:
		return cacheEntry{}, errUnavailable
	}
	var body userResponse
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return cacheEntry{}, errUnavailable
	}
	fields := body.userFields
	if body.User != nil {
		fields = *body.User
	}
	isOwner := fields.EmailVerified && strings.EqualFold(fields.Email, o.cfg.OwnerEmail)
	return cacheEntry{email: fields.Email, isOwner: isOwner, expires: expires}, nil
}
