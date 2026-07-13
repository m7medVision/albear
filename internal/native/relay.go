package native

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"

	transport "albear/internal/infrastructure/transport/noise"
)

// wireMsg is the JSON shape exchanged with the extension over native
// messaging. The frame field carries one opaque Noise frame payload,
// base64-encoded. The relay never inspects frame contents after the hello.
type wireMsg struct {
	Frame string `json:"frame,omitempty"`
	Error string `json:"error,omitempty"`
}

var ErrModeRefused = errors.New("native: hello mode refused by relay")

// Relay pumps frames between the extension (native messaging on in/out) and
// vaultd (Unix socket at socketPath) until either side closes.
//
// The first extension frame is the plaintext Noise hello. The relay enforces
// that its mode is "pair" or "paired": the "cli" auto-authorization mode is
// reserved for direct same-user connections and must never arrive through
// the browser path (PRD 12.3). Everything after the hello is ciphertext the
// relay cannot read or modify without breaking the handshake or AEAD.
func Relay(in io.Reader, out io.Writer, socketPath string) error {
	// First message: the hello.
	var first wireMsg
	if err := ReadMessage(in, &first); err != nil {
		return err
	}
	helloRaw, err := base64.StdEncoding.DecodeString(first.Frame)
	if err != nil {
		WriteMessage(out, wireMsg{Error: "bad frame encoding"})
		return ErrBadMessage
	}
	var hello transport.Hello
	if err := json.Unmarshal(helloRaw, &hello); err != nil ||
		(hello.Mode != transport.ModePairing && hello.Mode != transport.ModePaired) {
		WriteMessage(out, wireMsg{Error: "hello mode refused"})
		return ErrModeRefused
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		WriteMessage(out, wireMsg{Error: "daemon unavailable"})
		return err
	}
	defer conn.Close()

	if err := transport.WriteFrame(conn, helloRaw); err != nil {
		return err
	}

	// Daemon → extension.
	done := make(chan error, 1)
	go func() {
		for {
			frame, err := transport.ReadFrame(conn)
			if err != nil {
				done <- err
				return
			}
			if err := WriteMessage(out, wireMsg{Frame: base64.StdEncoding.EncodeToString(frame)}); err != nil {
				done <- err
				return
			}
		}
	}()

	// Extension → daemon.
	for {
		var msg wireMsg
		if err := ReadMessage(in, &msg); err != nil {
			conn.Close()
			<-done
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		frame, err := base64.StdEncoding.DecodeString(msg.Frame)
		if err != nil {
			conn.Close()
			<-done
			return ErrBadMessage
		}
		if err := transport.WriteFrame(conn, frame); err != nil {
			<-done
			return err
		}
	}
}
