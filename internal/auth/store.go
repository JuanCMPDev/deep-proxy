package auth

import "sync"

// HeaderStore holds the per-session credentials that DeepSeek's web client
// recomputes dynamically (Token + Cookie + x-hif-leim). It is safe for
// concurrent access by multiple goroutines: many readers (request handlers)
// and one writer (the Refresher).
type HeaderStore struct {
	mu      sync.RWMutex
	token   string
	cookie  string
	hifLeim string
}

// NewHeaderStore creates a store seeded with initial values.
func NewHeaderStore(token, cookie, hifLeim string) *HeaderStore {
	return &HeaderStore{token: token, cookie: cookie, hifLeim: hifLeim}
}

// Get returns the current Token, Cookie and x-hif-leim values atomically.
func (s *HeaderStore) Get() (token, cookie, hifLeim string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token, s.cookie, s.hifLeim
}

// Set replaces values atomically. Empty values are NOT written so a partial
// refresh result cannot blank out a previously-good credential.
func (s *HeaderStore) Set(token, cookie, hifLeim string) {
	if token == "" && cookie == "" && hifLeim == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if token != "" {
		s.token = token
	}
	if cookie != "" {
		s.cookie = cookie
	}
	if hifLeim != "" {
		s.hifLeim = hifLeim
	}
}
