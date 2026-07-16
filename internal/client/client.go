// Package client is the Go client for vaultd: it dials the Unix socket, runs
// the Noise handshake, and exchanges protocol envelopes. Used by the vault
// CLI and the vault-native bridge.
package client

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	flynnnoise "github.com/flynn/noise"

	"github.com/m7medVision/albear/internal/adapters/protocol"
	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	transport "github.com/m7medVision/albear/internal/infrastructure/transport/noise"
)

// APIError is a structured daemon error.
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

var ErrDaemonUnavailable = errors.New("client: daemon unavailable")

type Client struct {
	netConn net.Conn
	conn    *transport.Conn
	counter uint64
}

// DialCLI connects with same-user CLI auto-authorization: an ephemeral static
// key and the "cli" hello mode over a peer-credential-checked socket.
func DialCLI(socketPath string) (*Client, error) {
	static, err := transport.GenerateStaticKey()
	if err != nil {
		return nil, err
	}
	return dial(socketPath, static, transport.Hello{Version: 1, Mode: "cli"}, nil, nil)
}

// DialPairing connects on the unpaired pairing channel.
func DialPairing(socketPath string, static flynnnoise.DHKey) (*Client, error) {
	return dial(socketPath, static, transport.Hello{Version: 1, Mode: transport.ModePairing}, nil, nil)
}

// DialPaired connects as a registered client. The PSK is derived from the raw
// credential; the daemon static key is verified when pinned.
func DialPaired(socketPath string, static flynnnoise.DHKey, clientID string, credential, pinnedDaemonKey []byte) (*Client, error) {
	psk := crypto.CredentialVerifier(credential)
	hello := transport.Hello{Version: 1, Mode: transport.ModePaired, ClientID: clientID}
	return dial(socketPath, static, hello, psk, pinnedDaemonKey)
}

func dial(socketPath string, static flynnnoise.DHKey, hello transport.Hello, psk, pinnedServer []byte) (*Client, error) {
	nc, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDaemonUnavailable, err)
	}
	conn, _, err := transport.ClientHandshake(nc, static, hello, psk, pinnedServer)
	if err != nil {
		nc.Close()
		return nil, err
	}
	return &Client{netConn: nc, conn: conn}, nil
}

func (c *Client) Close() error { return c.netConn.Close() }

// Call sends one request and decodes the response data into out (which may
// be nil). Daemon-reported failures come back as *APIError.
func (c *Client) Call(operation string, payload, out any) error {
	c.counter++
	req := protocol.Request{
		ProtocolVersion: protocol.Version,
		RequestID:       fmt.Sprintf("req-%d", c.counter),
		Operation:       operation,
	}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		req.Payload = raw
	}
	rawReq, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := c.conn.Send(rawReq); err != nil {
		return fmt.Errorf("%w: %v", ErrDaemonUnavailable, err)
	}
	rawResp, err := c.conn.Recv()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDaemonUnavailable, err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(rawResp, &resp); err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error == nil {
			return errors.New("client: malformed error response")
		}
		return &APIError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	if out != nil && len(resp.Data) > 0 {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

// Identity is a paired client's stored identity.
type Identity struct {
	ClientID        string `json:"clientId"`
	Credential      string `json:"credential"`
	DaemonStaticKey string `json:"daemonStaticKey"`
	StaticPublic    string `json:"staticPublic"`
	StaticPrivate   string `json:"staticPrivate"`
}

func (i Identity) CredentialBytes() ([]byte, error) { return hex.DecodeString(i.Credential) }
func (i Identity) DaemonKeyBytes() ([]byte, error)  { return hex.DecodeString(i.DaemonStaticKey) }
func (i Identity) StaticKey() (flynnnoise.DHKey, error) {
	pub, err := hex.DecodeString(i.StaticPublic)
	if err != nil {
		return flynnnoise.DHKey{}, err
	}
	priv, err := hex.DecodeString(i.StaticPrivate)
	if err != nil {
		return flynnnoise.DHKey{}, err
	}
	return flynnnoise.DHKey{Public: pub, Private: priv}, nil
}
