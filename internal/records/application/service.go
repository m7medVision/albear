// Package application implements the Secret Catalog context. It is split
// CQRS-style: commands.go mutates records, queries.go only reads. All
// encryption keys arrive through the KeySource port from the Vault Security
// context; this package never derives or persists keys itself.
package application

import (
	shared "albear/internal/shared/domain"
	vaultapp "albear/internal/vault/application"

	"albear/internal/infrastructure/sqlite"
)

// KeySource is the temporary cryptographic capability granted by the Vault
// Security context while the vault is unlocked (PRD 9.2).
type KeySource interface {
	Keys() (*vaultapp.Keyring, error)
	VaultInfo() (vaultID []byte, formatVersion, keyVersion uint32, err error)
}

// Clock is re-exported for injection.
type Clock = shared.Clock

// KeyringRef aliases the vault keyring for use in this context.
type KeyringRef = vaultapp.Keyring

// Service is the record catalog service: one shared in-memory index plus the
// command and query sides.
type Service struct {
	store *sqlite.Store
	keys  KeySource
	clock Clock
	index *Index
}

func NewService(store *sqlite.Store, keys KeySource, clock Clock) *Service {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &Service{store: store, keys: keys, clock: clock, index: NewIndex()}
}

// Index exposes the metadata index (read-only usage expected).
func (s *Service) Index() *Index { return s.index }

// ClearIndex destroys the in-memory index; wired to the vault OnLock hook.
func (s *Service) ClearIndex() { s.index.Clear() }
