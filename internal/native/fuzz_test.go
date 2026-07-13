package native

import (
	"bytes"
	"testing"
)

// FuzzReadNativeMessage: native-messaging framing must never panic or
// over-allocate on hostile input (PRD 26.4).
func FuzzReadNativeMessage(f *testing.F) {
	var seed bytes.Buffer
	WriteMessage(&seed, map[string]string{"frame": "aGk="})
	f.Add(seed.Bytes())
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		var v map[string]any
		_ = ReadMessage(bytes.NewReader(data), &v)
	})
}
