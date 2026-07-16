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
