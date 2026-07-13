package domain

import (
	"time"

	shared "albear/internal/shared/domain"
)

type VaultState int

const (
	StateLocked VaultState = iota
	StateUnlocked
)

const FormatVersion uint32 = 1

// LockPolicy controls automatic locking.
type LockPolicy struct {
	AutoLockAfter time.Duration
}

var DefaultLockPolicy = LockPolicy{AutoLockAfter: 5 * time.Minute}

// Vault is the vault aggregate. Lock-state transitions and the monotonically
// increasing epoch live here; key material never does.
type Vault struct {
	ID                    shared.ID
	State                 VaultState
	FormatVersion         uint32
	ActiveEnvelopeVersion uint32
	LockPolicy            LockPolicy
	Epoch                 uint64
}

// Unlock transitions to unlocked and bumps the epoch so that sessions are
// bound to this specific unlocked period.
func (v *Vault) Unlock() {
	v.State = StateUnlocked
	v.Epoch++
}

// Lock transitions to locked and bumps the epoch, which invalidates every
// session issued during the previous epoch immediately.
func (v *Vault) Lock() {
	v.State = StateLocked
	v.Epoch++
}

func (v *Vault) IsUnlocked() bool { return v.State == StateUnlocked }
