package routing

import (
	"sync"
	"time"
)

const maxPins = 50_000

type pin struct {
	entryID string
	keyID   string
	expires time.Time
}

func (p pin) is(a attempt) bool { return p.entryID == a.t.entry.ID && p.keyID == a.key.ID }

// pinStore maps a conversation key to its pinned target; in memory only. Its mutex is a leaf: never held while taking Engine.mu.
type pinStore struct {
	mu sync.Mutex
	m  map[string]pin
}

func newPinStore() *pinStore { return &pinStore{m: map[string]pin{}} }

func (s *pinStore) get(key string, now time.Time) (pin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[key]
	if ok && !now.Before(p.expires) {
		delete(s.m, key)
		return pin{}, false
	}
	return p, ok
}

func (s *pinStore) set(key string, p pin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; !ok && len(s.m) >= maxPins {
		s.evictSoonest()
	}
	s.m[key] = p
}

// drop removes the pin only while it still names the failed target.
func (s *pinStore) drop(key string, failed pin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.m[key]; ok && p.entryID == failed.entryID && p.keyID == failed.keyID {
		delete(s.m, key)
	}
}

// evict removes expired pins.
func (s *pinStore) evict(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, p := range s.m {
		if !now.Before(p.expires) {
			delete(s.m, k)
		}
	}
}

// evictSoonest frees one slot; the caller holds s.mu.
func (s *pinStore) evictSoonest() {
	var oldest string
	var at time.Time
	for k, p := range s.m {
		if oldest == "" || p.expires.Before(at) {
			oldest, at = k, p.expires
		}
	}
	delete(s.m, oldest)
}
