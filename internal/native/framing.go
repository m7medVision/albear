// Package native implements the vault-native bridge: Chrome Native Messaging
// framing, extension-origin validation, and the blind relay that forwards
// opaque Noise frames between the extension and vaultd (PRD 11.3).
package native

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

// Chrome native messages are 4-byte host-endian (little endian on supported
// platforms) length-prefixed JSON, limited to 1 MB host→browser.
const MaxNativeMessage = 1024 * 1024

var (
	ErrMessageTooLarge = errors.New("native: message exceeds limit")
	ErrBadMessage      = errors.New("native: malformed message")
)

// ReadMessage reads one native-messaging JSON message into v.
func ReadMessage(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n > MaxNativeMessage {
		return ErrMessageTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return ErrBadMessage
	}
	if err := json.Unmarshal(buf, v); err != nil {
		return ErrBadMessage
	}
	return nil
}

// WriteMessage writes v as one native-messaging JSON message.
func WriteMessage(w io.Writer, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(buf) > MaxNativeMessage {
		return ErrMessageTooLarge
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(buf)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}
