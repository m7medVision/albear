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

// Hard maximums. Cost parameters are read back from the key envelope on every
// unlock, so a tampered row could otherwise name 64 GiB of memory and turn an
// unlock attempt into an out-of-memory kill. These bounds sit far above the
// default profile (128 MiB / 3 / 4) and above any plausible hardening of it,
// so they cost a legitimate vault nothing.
const (
	MaxMemoryKiB   = 1 << 20 // 1 GiB
	MaxIterations  = 20
	MaxParallelism = 16
)

// DefaultKDFParams is the initial profile: 128 MiB, 3 iterations, 4 lanes.
var DefaultKDFParams = KDFParams{
	MemoryKiB:   128 * 1024,
	Iterations:  3,
	Parallelism: 4,
}

var ErrWeakKDFParams = errors.New("crypto: KDF parameters below hard minimum")

// ErrExcessiveKDFParams is returned for parameters above the hard maximums.
// It is separate from ErrWeakKDFParams because the causes differ: too weak
// means a bad profile, too large means the envelope is not to be trusted.
var ErrExcessiveKDFParams = errors.New("crypto: KDF parameters above hard maximum")

// Validate bounds the cost parameters from both sides. DeriveKEK calls it
// before touching Argon2, so unlock, password change, and verification all
// inherit the guard against a tampered envelope.
func (p KDFParams) Validate() error {
	if p.MemoryKiB < MinMemoryKiB || p.Iterations < MinIterations || p.Parallelism < 1 {
		return ErrWeakKDFParams
	}
	if p.MemoryKiB > MaxMemoryKiB || p.Iterations > MaxIterations || p.Parallelism > MaxParallelism {
		return ErrExcessiveKDFParams
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
