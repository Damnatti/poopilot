package pty

import (
	"fmt"
	"sync"

	"github.com/denismelnikov/poopilot/internal/protocol"
)

// Manager manages multiple PTY sessions.
type Manager struct {
	sessions    map[string]*Session
	mu          sync.RWMutex
	maxSessions int
}

// NewManager creates a session manager with the given max session limit.
func NewManager(maxSessions int) *Manager {
	return &Manager{
		sessions:    make(map[string]*Session),
		maxSessions: maxSessions,
	}
}

// Create creates and starts a new PTY session.
func (m *Manager) Create(command string, args []string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.sessions) >= m.maxSessions {
		return nil, fmt.Errorf("max sessions (%d) reached", m.maxSessions)
	}

	s := NewSession(command, args)
	if err := s.Start(); err != nil {
		return nil, err
	}

	m.sessions[s.ID] = s
	return s, nil
}

// Get returns a session by ID.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// List returns info about all sessions.
func (m *Manager) List() []protocol.SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]protocol.SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, s.Info())
	}
	return infos
}

// Close closes and removes a specific session.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	err := s.Close()
	delete(m.sessions, id)
	return err
}

// CloseAll closes all sessions.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, s := range m.sessions {
		s.Close()
		delete(m.sessions, id)
	}
}

// Count returns the number of active sessions.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
