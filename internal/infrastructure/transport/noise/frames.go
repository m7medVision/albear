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
// (native.MaxNativeMessage) once the relay has base64-encoded it and wrapped
// it in {"frame":"..."}.
//
// The budget: base64 costs 4 bytes per 3, so the frame ceiling must satisfy
// ceil(n/3)*4 + 12 <= 1048576, giving n <= 786420. The previous 768 KiB
// (786432) encoded to exactly 1048576 and then overflowed on the wrapper, so
// a maximum-size frame could not be relayed at all. 750 KiB encodes to
// 1024000 and leaves ~24 KiB of headroom.
//
// Keep this in step with MAX_FRAME_SIZE in desktop/src/main/frames.ts.
const MaxFrameSize = 750 * 1024

var (
	ErrFrameTooLarge = errors.New("noise: frame exceeds maximum size")
	ErrBadFrame      = errors.New("noise: malformed frame")
)

// WriteFrame writes one length-prefixed frame.
func WriteFrame(w io.Writer, payload []byte) error {
	// A zero-length frame is never legitimate: every Noise transport message
	// carries at least its 16-byte auth tag, and the hello is JSON. Refusing
	// it here and in ReadFrame keeps a meaningless frame off the wire in both
	// directions rather than leaving each caller to notice.
	if len(payload) == 0 {
		return ErrBadFrame
	}
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
	if n == 0 {
		return nil, ErrBadFrame
	}
	if n > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, ErrBadFrame
	}
	return payload, nil
}
