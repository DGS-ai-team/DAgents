// Package uifocus records Web UI focus claims used to suppress duplicate toasts.
package uifocus

import (
	"strings"
	"sync"
	"time"
)

// DefaultTTL is the default lifetime of a focus claim.
const DefaultTTL = 90 * time.Second

type Store struct {
	mu     sync.Mutex
	claims map[string]claim
}

type claim struct {
	agentID   string
	expiresAt time.Time
}

func NewStore() *Store {
	return &Store{claims: make(map[string]claim)}
}

// Report sets or clears a focus claim owned by one Web UI source.
func (s *Store) Report(sourceID, agentID string, ttl time.Duration) {
	if s == nil {
		return
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	sourceID = strings.TrimSpace(sourceID)
	agentID = strings.TrimSpace(agentID)
	if sourceID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureClaimsLocked()
	s.pruneExpiredLocked()
	if agentID == "" {
		delete(s.claims, sourceID)
		return
	}
	s.claims[sourceID] = claim{agentID: agentID, expiresAt: time.Now().Add(ttl)}
}

// IsFocused reports whether any live Web UI source is viewing the Agent.
func (s *Store) IsFocused(agentID string) bool {
	if s == nil {
		return false
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureClaimsLocked()
	s.pruneExpiredLocked()
	for _, item := range s.claims {
		if item.agentID == agentID {
			return true
		}
	}
	return false
}

// FocusedSession returns one live focused Agent for diagnostics and tests.
func (s *Store) FocusedSession() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureClaimsLocked()
	s.pruneExpiredLocked()
	for _, item := range s.claims {
		return item.agentID
	}
	return ""
}

func (s *Store) ensureClaimsLocked() {
	if s.claims == nil {
		s.claims = make(map[string]claim)
	}
}

func (s *Store) pruneExpiredLocked() {
	now := time.Now()
	for sourceID, item := range s.claims {
		if !now.Before(item.expiresAt) {
			delete(s.claims, sourceID)
		}
	}
}
