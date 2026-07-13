package crypto

import (
	"errors"

	"golang.org/x/crypto/argon2"
)

// KDFParams are the Argon2id cost parameters stored alongside a key envelope.
type KDFParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// Hard minimums from the PRD (section 16.4); the schema enforces the same.
const (
	MinMemoryKiB  = 64 * 1024
	MinIterations = 3
)

// DefaultKDFParams is the initial profile: 128 MiB, 3 iterations, 4 lanes.
var DefaultKDFParams = KDFParams{
	MemoryKiB:   128 * 1024,
	Iterations:  3,
	Parallelism: 4,
}

var ErrWeakKDFParams = errors.New("crypto: KDF parameters below hard minimum")

func (p KDFParams) Validate() error {
	if p.MemoryKiB < MinMemoryKiB || p.Iterations < MinIterations || p.Parallelism < 1 {
		return ErrWeakKDFParams
	}
	return nil
}

// DeriveKEK derives the 32-byte key-encryption key from a master password.
// The password stays a byte slice end to end; callers zero it afterwards.
func DeriveKEK(password, salt []byte, p KDFParams) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(salt) < SaltSize {
		return nil, errors.New("crypto: salt too short")
	}
	return argon2.IDKey(password, salt, p.Iterations, p.MemoryKiB, p.Parallelism, KeySize), nil
}
