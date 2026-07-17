package native

import (
	"bytes"
	"encoding/base64"
	"testing"

	transport "github.com/m7medVision/albear/internal/infrastructure/transport/noise"
)

// TestMaxFrameFitsNativeMessage: the relay base64-encodes a Noise frame and
// wraps it in JSON, so the frame ceiling must leave room for both. A frame the
// daemon is willing to emit but the relay cannot carry would strand the
// extension on large payloads with no way to notice until it happened.
//
// This test lives here rather than in the noise package because it is the
// relay's cap that constrains the transport, and the dependency only points
// this way.
func TestMaxFrameFitsNativeMessage(t *testing.T) {
	frame := make([]byte, transport.MaxFrameSize)
	var buf bytes.Buffer
	if err := WriteMessage(&buf, wireMsg{Frame: base64.StdEncoding.EncodeToString(frame)}); err != nil {
		t.Fatalf("a maximum-size frame cannot be relayed: %v", err)
	}
	// The 4-byte length prefix is framing, not message: Chrome's limit applies
	// to the JSON body, which is what WriteMessage bounds.
	if body := buf.Len() - 4; body > MaxNativeMessage {
		t.Fatalf("relayed message %d exceeds the %d cap", body, MaxNativeMessage)
	}

	// And it round-trips back through the reader.
	var got wireMsg
	if err := ReadMessage(&buf, &got); err != nil {
		t.Fatalf("maximum-size message did not read back: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Frame)
	if err != nil || len(decoded) != transport.MaxFrameSize {
		t.Fatalf("frame did not survive the relay: %d bytes, %v", len(decoded), err)
	}
}
