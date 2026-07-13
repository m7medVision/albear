package crypto

import (
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

var (
	ErrDecryptFailed = errors.New("crypto: authentication failed")
	ErrBadNonce      = errors.New("crypto: nonce must be 24 bytes")
	ErrBadKey        = errors.New("crypto: key must be 32 bytes")
)

// Seal encrypts plaintext with XChaCha20-Poly1305 under the given key, a fresh
// 24-byte nonce, and associated data binding the ciphertext to its context.
func Seal(key, nonce, plaintext, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key, nonce)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nonce, plaintext, aad), nil
}

// Open decrypts and authenticates. Any failure is reported as a single generic
// error so callers cannot distinguish tampering modes.
func Open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key, nonce)
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return pt, nil
}

func newAEAD(key, nonce []byte) (interface {
	Seal(dst, nonce, plaintext, aad []byte) []byte
	Open(dst, nonce, ciphertext, aad []byte) ([]byte, error)
}, error) {
	if len(key) != KeySize {
		return nil, ErrBadKey
	}
	if len(nonce) != NonceSize {
		return nil, ErrBadNonce
	}
	return chacha20poly1305.NewX(key)
}
