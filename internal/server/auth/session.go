package auth

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"
)

type SessionStore struct {
	mu          sync.RWMutex
	tokens      map[string]struct{}
	adminTokens map[string]struct{}
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		tokens:      make(map[string]struct{}),
		adminTokens: make(map[string]struct{}),
	}
}

func (s *SessionStore) NewToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		log.Fatal("Failed to generate secure token: crypto/rand unavailable")
		return ""
	}
	return hex.EncodeToString(bytes)
}

func (s *SessionStore) Add(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = struct{}{}
}

func (s *SessionStore) Valid(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.tokens[token]
	return exists
}

func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tokens)
}

func (s *SessionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = make(map[string]struct{})
	s.adminTokens = make(map[string]struct{})
}

func (s *SessionStore) AddAdmin(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adminTokens[token] = struct{}{}
}

func (s *SessionStore) ValidAdmin(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.adminTokens[token]
	return exists
}

func (s *SessionStore) CountAdmin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.adminTokens)
}

func (s *SessionStore) RemoveAdmin(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.adminTokens, token)
}
