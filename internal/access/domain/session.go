package domain

import (
	"time"

	shared "github.com/m7medVision/albear/internal/shared/domain"
)

// Session is an in-memory authorization for one connected client. Sessions
// never touch the database and never survive a daemon restart.
type Session struct {
	ID           shared.ID
	ClientID     shared.ID
	Capabilities CapabilitySet
	CreatedAt    time.Time
	LastActivity time.Time
	ExpiresAt    time.Time
	VaultEpoch   uint64
}

// ValidAt reports whether the session may act at the given time and epoch. A
// session from an earlier epoch is dead the moment the vault locks.
func (s *Session) ValidAt(now time.Time, epoch uint64) bool {
	if s.VaultEpoch != epoch {
		return false
	}
	if !s.ExpiresAt.IsZero() && now.After(s.ExpiresAt) {
		return false
	}
	return true
}

// Authorize checks a capability against the session's set.
func (s *Session) Authorize(c Capability) error {
	if !s.Capabilities.Has(c) {
		return shared.ErrAuthorizationDeny
	}
	return nil
}
