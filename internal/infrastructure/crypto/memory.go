package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"runtime"
)

// Zero overwrites a secret buffer best-effort before release. Go's GC cannot
// guarantee erasure of every copy; this narrows the window (PRD section 16.8).
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(b)
}

// CredentialVerifier hashes a raw client credential into the stored verifier.
// The verifier is also the Noise PSK: the daemon never stores the raw
// credential, and a database thief who learns the verifier gains a transport
// PSK but no vault key material.
func CredentialVerifier(credential []byte) []byte {
	sum := sha256.Sum256(credential)
	return sum[:]
}

// VerifierEqual compares verifiers in constant time.
func VerifierEqual(a, b []byte) bool {
	return hmac.Equal(a, b)
}
