// Package noise implements albear's transport encryption (PRD 12.4):
// Noise_XXpsk3_25519_ChaChaPoly_SHA256 for paired clients, Noise_XX for the
// pairing channel, length-prefixed frames, and counter-based rekeying.
package noise

import (
	"encoding/binary"
	"errors"
	"io"
)

// MaxFrameSize keeps every frame under Chrome's 1 MB native-messaging limit
// with room for relay overhead.
const MaxFrameSize = 768 * 1024

var (
	ErrFrameTooLarge = errors.New("noise: frame exceeds maximum size")
	ErrBadFrame      = errors.New("noise: malformed frame")
)

// WriteFrame writes one length-prefixed frame.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrameSize {
		return ErrFrameTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads one length-prefixed frame, enforcing the size limit before
// allocating.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, ErrBadFrame
	}
	return payload, nil
}
