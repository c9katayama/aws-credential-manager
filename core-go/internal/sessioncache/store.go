package sessioncache

import (
	"sync"
	"time"
)

type Registration struct {
	ClientID              string
	ClientSecret          string
	ClientSecretExpiresAt time.Time
}

type Session struct {
	Registration   Registration
	AccessToken    string
	AccessExpiry   time.Time
	RefreshToken   string
	LastBrowserURL string
}

type Store struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func New() *Store {
	return &Store{sessions: map[string]Session{}}
}

func (s *Store) Get(key string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[key]
	return session, ok
}

func (s *Store) Put(key string, session Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[key] = session
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}
