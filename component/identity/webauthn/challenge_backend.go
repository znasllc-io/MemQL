package webauthn

import (
	"sync"
	"time"
)

// ChallengeBackend persists server-side state under a scoped handle digest.
// Take MUST atomically remove and return the entry, including expired entries;
// ChallengeStore validates only after consumption. An unavailable backend must
// return an error, never fall back to a replica-local copy.
type ChallengeBackend interface {
	Put(key string, entry *ChallengeEntry, now time.Time) error
	Take(key string) (*ChallengeEntry, error)
}

type memoryChallengeBackend struct {
	mu      sync.Mutex
	entries map[string]*ChallengeEntry
}

// NewMemoryChallengeBackend is for isolated ceremonies and unit fixtures.
// Identity's HTTP server uses NewPostgresChallengeBackend by default.
func NewMemoryChallengeBackend() ChallengeBackend {
	return &memoryChallengeBackend{entries: make(map[string]*ChallengeEntry)}
}

func (s *memoryChallengeBackend) Put(key string, entry *ChallengeEntry, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.entries {
		if !now.Before(old.ExpiresAt) {
			delete(s.entries, id)
		}
	}
	s.entries[key] = entry
	return nil
}

func (s *memoryChallengeBackend) Take(key string) (*ChallengeEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[key]
	delete(s.entries, key)
	if entry == nil {
		return nil, ErrChallengeNotFound
	}
	return entry, nil
}
