package crypto

import (
	"crypto/rand"
	"fmt"
)

const (
	IDSize         = 16
	KeySize        = 32
	NonceSize      = 24
	SaltSize       = 16
	CredentialSize = 32
)

// RandomBytes returns n cryptographically random bytes. It fails hard when the
// system random source fails: no fallback source is acceptable.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("crypto: random source failure: %w", err)
	}
	return b, nil
}

func NewID() ([]byte, error)         { return RandomBytes(IDSize) }
func NewKey() ([]byte, error)        { return RandomBytes(KeySize) }
func NewNonce() ([]byte, error)      { return RandomBytes(NonceSize) }
func NewSalt() ([]byte, error)       { return RandomBytes(SaltSize) }
func NewCredential() ([]byte, error) { return RandomBytes(CredentialSize) }
