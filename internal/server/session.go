package server

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"
)

// SessionStore manages in-memory session tokens
type SessionStore struct {
	mu          sync.RWMutex
	tokens      map[string]struct{}
	adminTokens map[string]struct{}
}

// NewSessionStore creates a new session store
func NewSessionStore() *SessionStore {
	return &SessionStore{
		tokens:      make(map[string]struct{}),
		adminTokens: make(map[string]struct{}),
	}
}

// NewToken generates a cryptographically secure random token
func (s *SessionStore) NewToken() string {
	// Generate 32 random bytes
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// If crypto/rand fails, abort startup - never use weak fallback
		log.Fatal("Failed to generate secure token: crypto/rand unavailable")
		return ""
	}
	return hex.EncodeToString(bytes)
}

// Add adds a token to the session store
func (s *SessionStore) Add(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = struct{}{}
}

// Valid checks if a token exists in the session store
func (s *SessionStore) Valid(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.tokens[token]
	return exists
}

// Count returns the number of active sessions (for testing)
func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tokens)
}

// Clear removes all tokens from the session store (for testing)
func (s *SessionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = make(map[string]struct{})
	s.adminTokens = make(map[string]struct{})
}

// AddAdmin adds an admin token to the session store
func (s *SessionStore) AddAdmin(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adminTokens[token] = struct{}{}
}

// ValidAdmin checks if an admin token exists in the session store
func (s *SessionStore) ValidAdmin(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.adminTokens[token]
	return exists
}

// CountAdmin returns the number of active admin sessions (for testing)
func (s *SessionStore) CountAdmin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.adminTokens)
}

// RemoveAdmin removes an admin token from the session store
func (s *SessionStore) RemoveAdmin(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.adminTokens, token)
}
