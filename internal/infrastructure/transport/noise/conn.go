package noise

import (
	"errors"
	"io"
	"sync"

	"github.com/flynn/noise"
)

// RekeyInterval is how many messages a cipher state may encrypt or decrypt
// before it is rekeyed. Both sides apply the same deterministic rule per
// direction, so no rekey negotiation is needed and nonce exhaustion is
// structurally impossible.
const RekeyInterval = 4096

var ErrTransportAEAD = errors.New("noise: transport authentication failed")

// Conn is an established Noise transport session over an underlying stream.
// Every payload is AEAD-protected; any tampered frame fails Recv.
type Conn struct {
	rw io.ReadWriter

	sendMu    sync.Mutex
	send      *noise.CipherState
	sendCount uint64

	recvMu    sync.Mutex
	recv      *noise.CipherState
	recvCount uint64
}

func newConn(rw io.ReadWriter, send, recv *noise.CipherState) *Conn {
	return &Conn{rw: rw, send: send, recv: recv}
}

// Send encrypts and writes one message.
func (c *Conn) Send(payload []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	ct, err := c.send.Encrypt(nil, nil, payload)
	if err != nil {
		return err
	}
	c.sendCount++
	if c.sendCount%RekeyInterval == 0 {
		c.send.Rekey()
	}
	return WriteFrame(c.rw, ct)
}

// Recv reads and decrypts one message. Any authentication failure is fatal
// for the session (PRD 19.1 Level 2).
func (c *Conn) Recv() ([]byte, error) {
	c.recvMu.Lock()
	defer c.recvMu.Unlock()
	ct, err := ReadFrame(c.rw)
	if err != nil {
		return nil, err
	}
	pt, err := c.recv.Decrypt(nil, nil, ct)
	if err != nil {
		return nil, ErrTransportAEAD
	}
	c.recvCount++
	if c.recvCount%RekeyInterval == 0 {
		c.recv.Rekey()
	}
	return pt, nil
}
