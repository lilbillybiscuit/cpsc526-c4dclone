package src

import (
	"sync"
	"time"
)

type SessionState struct {
	NodeID         string
	NodeURL        string
	Status         string
	Rank           int
	LastSeen       time.Time
	ReconnectCount int
	IsConnected    bool
}

type SessionManager struct {
	sessions map[string]*SessionState
	mu       sync.RWMutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionState),
	}
}

func (sm *SessionManager) RegisterSession(nodeID, nodeURL string, status string, rank int) *SessionState {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[nodeID]
	if exists {
		// Update existing session
		session.NodeURL = nodeURL
		session.LastSeen = time.Now()
		session.IsConnected = true
		session.ReconnectCount++
		// Maintain existing status and rank unless explicitly changed
		if status != "" {
			session.Status = status
		}
		if rank >= 0 {
			session.Rank = rank
		}
		return session
	}

	// Create new session
	session = &SessionState{
		NodeID:         nodeID,
		NodeURL:        nodeURL,
		Status:         status,
		Rank:           rank,
		LastSeen:       time.Now(),
		ReconnectCount: 0,
		IsConnected:    true,
	}
	sm.sessions[nodeID] = session
	return session
}

func (sm *SessionManager) GetSession(nodeID string) (*SessionState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, exists := sm.sessions[nodeID]
	if exists {
		// Return a copy to prevent concurrent modification
		sessionCopy := *session
		return &sessionCopy, true
	}
	return nil, false
}

func (sm *SessionManager) UpdateSession(nodeID string, updateFn func(*SessionState)) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[nodeID]; exists {
		updateFn(session)
		return true
	}
	return false
}

func (sm *SessionManager) DisconnectSession(nodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[nodeID]; exists {
		session.IsConnected = false
		session.LastSeen = time.Now()
	}
}

func (sm *SessionManager) CleanupSessions(timeout time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cutoff := time.Now().Add(-timeout)
	for nodeID, session := range sm.sessions {
		if !session.IsConnected && session.LastSeen.Before(cutoff) {
			delete(sm.sessions, nodeID)
		}
	}
}
