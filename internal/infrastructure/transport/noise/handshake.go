package noise

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"

	"github.com/flynn/noise"
)

var cipherSuite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)

// Handshake modes carried in the plaintext hello. ModeCLI is Noise_XX like
// pairing; the daemon grants it CLI capabilities only on peer-credential
// verified direct connections, and vault-native refuses to relay it.
const (
	ModePaired  = "paired"
	ModePairing = "pair"
	ModeCLI     = "cli"
)

var (
	ErrHandshakeFailed   = errors.New("noise: handshake failed")
	ErrStaticKeyMismatch = errors.New("noise: static key does not match pinned key")
	ErrUnknownMode       = errors.New("noise: unknown handshake mode")
	ErrProtocolViolation = errors.New("noise: protocol violation")
)

// Hello is the plaintext first frame. It carries no secrets: only which
// handshake pattern to run and, for paired clients, which PSK to load. Its
// exact bytes double as the Noise prologue, so any tampering with it breaks
// the handshake.
type Hello struct {
	Version  int    `json:"v"`
	Mode     string `json:"mode"`
	ClientID string `json:"clientId,omitempty"`
}

// GenerateStaticKey creates a new X25519 static keypair.
func GenerateStaticKey() (noise.DHKey, error) {
	return noise.DH25519.GenerateKeypair(rand.Reader)
}

func config(static noise.DHKey, initiator bool, psk, prologue []byte) noise.Config {
	cfg := noise.Config{
		CipherSuite:   cipherSuite,
		Random:        rand.Reader,
		Pattern:       noise.HandshakeXX,
		Initiator:     initiator,
		StaticKeypair: static,
		Prologue:      prologue,
	}
	if psk != nil {
		cfg.PresharedKey = psk
		cfg.PresharedKeyPlacement = 3
	}
	return cfg
}

// LookupFunc resolves a paired client's PSK and pinned static key from its
// hello. Returning an error aborts the handshake before any state is built.
type LookupFunc func(h Hello) (psk []byte, pinnedStatic []byte, err error)

// ServerHandshake runs the responder side. For ModePaired it loads the PSK
// via lookup and enforces the pinned static key after message 3. For
// ModePairing it runs plain XX and returns the client's static key for the
// pairing flow.
func ServerHandshake(rw io.ReadWriter, static noise.DHKey, lookup LookupFunc) (*Conn, *Hello, []byte, error) {
	helloRaw, err := ReadFrame(rw)
	if err != nil {
		return nil, nil, nil, err
	}
	var hello Hello
	if err := json.Unmarshal(helloRaw, &hello); err != nil || hello.Version != 1 {
		return nil, nil, nil, ErrProtocolViolation
	}

	var psk, pinned []byte
	switch hello.Mode {
	case ModePairing, ModeCLI:
	case ModePaired:
		psk, pinned, err = lookup(hello)
		if err != nil {
			return nil, nil, nil, err
		}
	default:
		return nil, nil, nil, ErrUnknownMode
	}

	hs, err := noise.NewHandshakeState(config(static, false, psk, helloRaw))
	if err != nil {
		return nil, nil, nil, err
	}

	// <- e
	msg1, err := ReadFrame(rw)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
		return nil, nil, nil, ErrHandshakeFailed
	}
	// -> e, ee, s, es
	msg2, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, nil, nil, ErrHandshakeFailed
	}
	if err := WriteFrame(rw, msg2); err != nil {
		return nil, nil, nil, err
	}
	// <- s, se (+psk3)
	msg3, err := ReadFrame(rw)
	if err != nil {
		return nil, nil, nil, err
	}
	_, csRecv, csSend, err := hs.ReadMessage(nil, msg3)
	if err != nil {
		return nil, nil, nil, ErrHandshakeFailed
	}

	remoteStatic := hs.PeerStatic()
	if hello.Mode == ModePaired && !equalKeys(remoteStatic, pinned) {
		return nil, nil, nil, ErrStaticKeyMismatch
	}
	return newConn(rw, csSend, csRecv), &hello, remoteStatic, nil
}

// ClientHandshake runs the initiator side. psk must be nil for ModePairing.
// If expectedServer is non-nil, the daemon's static key is verified against
// it (TOFU pinning after pairing).
func ClientHandshake(rw io.ReadWriter, static noise.DHKey, hello Hello, psk, expectedServer []byte) (*Conn, []byte, error) {
	helloRaw, err := json.Marshal(hello)
	if err != nil {
		return nil, nil, err
	}
	if err := WriteFrame(rw, helloRaw); err != nil {
		return nil, nil, err
	}

	hs, err := noise.NewHandshakeState(config(static, true, psk, helloRaw))
	if err != nil {
		return nil, nil, err
	}

	// -> e
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, nil, ErrHandshakeFailed
	}
	if err := WriteFrame(rw, msg1); err != nil {
		return nil, nil, err
	}
	// <- e, ee, s, es
	msg2, err := ReadFrame(rw)
	if err != nil {
		return nil, nil, err
	}
	if _, _, _, err := hs.ReadMessage(nil, msg2); err != nil {
		return nil, nil, ErrHandshakeFailed
	}
	// The daemon's static key is now known; verify the pin before sending
	// our own static key in message 3.
	serverStatic := hs.PeerStatic()
	if expectedServer != nil && !equalKeys(serverStatic, expectedServer) {
		return nil, nil, ErrStaticKeyMismatch
	}
	// -> s, se (+psk3)
	msg3, csSend, csRecv, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, nil, ErrHandshakeFailed
	}
	if err := WriteFrame(rw, msg3); err != nil {
		return nil, nil, err
	}
	return newConn(rw, csSend, csRecv), serverStatic, nil
}

func equalKeys(a, b []byte) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
