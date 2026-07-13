// Package domain holds the shared kernel: identifier types and domain errors
// used across bounded contexts. It imports nothing outside the standard
// library's encoding helpers.
package domain

import (
	"encoding/hex"
	"errors"
)

const idLen = 16

// ID is a 16-byte random identifier (vaults, records, clients).
type ID [idLen]byte

var ErrInvalidID = errors.New("domain: invalid identifier")

func IDFromBytes(b []byte) (ID, error) {
	var id ID
	if len(b) != idLen {
		return id, ErrInvalidID
	}
	copy(id[:], b)
	return id, nil
}

func IDFromString(s string) (ID, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return ID{}, ErrInvalidID
	}
	return IDFromBytes(b)
}

func (id ID) String() string { return hex.EncodeToString(id[:]) }
func (id ID) Bytes() []byte  { return append([]byte(nil), id[:]...) }
func (id ID) IsZero() bool   { return id == ID{} }
