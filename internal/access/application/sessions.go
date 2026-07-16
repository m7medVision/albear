package application

import (
	"sync"
	"time"

	domain "github.com/m7medVision/albear/internal/access/domain"
	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	shared "github.com/m7medVision/albear/internal/shared/domain"
)

// DefaultSessionTTL bounds how long an idle session may act.
const DefaultSessionTTL = 12 * time.Hour

// SessionManager holds all live sessions in memory. Locking the vault bumps
// the epoch, which invalidates every session issued earlier; InvalidateAll
// additionally drops them eagerly.
type SessionManager struct {
	mu       sync.Mutex
	clock    shared.Clock
	sessions map[shared.ID]*domain.Session
}

func NewSessionManager(clock shared.Clock) *SessionManager {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &SessionManager{clock: clock, sessions: map[shared.ID]*domain.Session{}}
}

// Issue creates a session bound to the current vault epoch.
func (m *SessionManager) Issue(clientID shared.ID, caps domain.CapabilitySet, epoch uint64) (*domain.Session, error) {
	idBytes, err := crypto.NewID()
	if err != nil {
		return nil, err
	}
	id, _ := shared.IDFromBytes(idBytes)
	now := m.clock.Now()
	s := &domain.Session{
		ID: id, ClientID: clientID, Capabilities: caps,
		CreatedAt: now, LastActivity: now, ExpiresAt: now.Add(DefaultSessionTTL),
		VaultEpoch: epoch,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = s
	return s, nil
}

// Validate returns the session if it is alive for the given epoch.
func (m *SessionManager) Validate(id shared.ID, epoch uint64) (*domain.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, shared.ErrAuthorizationDeny
	}
	now := m.clock.Now()
	if !s.ValidAt(now, epoch) {
		delete(m.sessions, id)
		return nil, shared.ErrAuthorizationDeny
	}
	s.LastActivity = now
	return s, nil
}

// Drop removes one session.
func (m *SessionManager) Drop(id shared.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// DropClient removes every session belonging to a client (revocation).
func (m *SessionManager) DropClient(clientID shared.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.ClientID == clientID {
			delete(m.sessions, id)
		}
	}
}

// InvalidateAll drops every session; wired to the vault OnLock hook.
func (m *SessionManager) InvalidateAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.sessions)
}

// Count reports live sessions (for doctor/status output).
func (m *SessionManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}
