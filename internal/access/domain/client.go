package domain

import (
	"time"

	shared "github.com/m7medVision/albear/internal/shared/domain"
)

type ClientKind int

const (
	KindCLI ClientKind = iota + 1
	KindChromeExtension
	KindChromeNativeHost
	KindAdministrativeTool
)

type ClientStatus int

const (
	StatusPending ClientStatus = iota + 1
	StatusApproved
	StatusRevoked
)

// Client is a registered daemon client. CredentialHash doubles as the Noise
// PSK; StaticKey is the pinned X25519 public key checked on every handshake.
type Client struct {
	ID             shared.ID
	Kind           ClientKind
	Status         ClientStatus
	Capabilities   CapabilitySet
	CredentialHash []byte
	StaticKey      []byte
	Label          string
	CreatedAt      time.Time
}

func (c *Client) IsApproved() bool { return c.Status == StatusApproved }

// IsPairable reports whether a kind may be requested over the pairing channel.
// It is an allowlist, so an unknown kind fails closed. KindCLI and
// KindAdministrativeTool map to the full administrative capability set and are
// excluded: pairing is reachable by any same-user process, so honouring those
// kinds there would let a caller request admin capabilities behind a prompt
// that a user might approve. Administrative access comes from the CLI's
// peer-credential path instead, never from pairing.
func (k ClientKind) IsPairable() bool {
	return k == KindChromeExtension || k == KindChromeNativeHost
}

// String names the kind for approval prompts, so an operator sees what they
// are granting rather than a bare integer.
func (k ClientKind) String() string {
	switch k {
	case KindCLI:
		return "cli"
	case KindChromeExtension:
		return "chrome-extension"
	case KindChromeNativeHost:
		return "chrome-native-host"
	case KindAdministrativeTool:
		return "administrative-tool"
	}
	return "unknown"
}

// DefaultCapabilities returns the least-privilege set for a client kind.
func DefaultCapabilities(kind ClientKind) CapabilitySet {
	switch kind {
	case KindCLI, KindAdministrativeTool:
		return CLICapabilities
	case KindChromeExtension:
		return ChromeCapabilities
	case KindChromeNativeHost:
		return RelayCapabilities
	}
	return 0
}
