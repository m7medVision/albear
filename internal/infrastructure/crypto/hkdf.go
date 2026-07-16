package crypto

import (
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

// Key-separation labels from the PRD key hierarchy (section 16.2).
const (
	LabelMetadata = "github.com/m7medVision/albear/v1/metadata"
	LabelSecrets  = "github.com/m7medVision/albear/v1/secrets"
	LabelAudit    = "github.com/m7medVision/albear/v1/audit"
	LabelBackup   = "github.com/m7medVision/albear/v1/backup"
)

// DeriveSubkey derives a purpose-bound 32-byte key from the root vault key.
func DeriveSubkey(rootKey []byte, label string) ([]byte, error) {
	if len(rootKey) != KeySize {
		return nil, ErrBadKey
	}
	out := make([]byte, KeySize)
	r := hkdf.New(sha256.New, rootKey, nil, []byte(label))
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, errors.New("crypto: hkdf expansion failed")
	}
	return out, nil
}
