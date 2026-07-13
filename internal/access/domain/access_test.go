package domain

import (
	"testing"
	"time"

	shared "albear/internal/shared/domain"
)

func TestCapabilitySet(t *testing.T) {
	s := NewCapabilitySet(CapRecordRead, CapRecordList)
	if !s.Has(CapRecordRead) || !s.Has(CapRecordList) {
		t.Fatal("granted capability missing")
	}
	if s.Has(CapVaultDestroy) {
		t.Fatal("ungranted capability present")
	}
}

func TestChromeCapabilitiesAreLeastPrivilege(t *testing.T) {
	// PRD 18.2: the extension must never receive these.
	forbidden := []Capability{
		CapBackupCreate, CapBackupRestore, CapClientAdmin,
		CapVaultDestroy, CapRecordReveal, CapRecordDelete, CapPasswordChange,
	}
	for _, c := range forbidden {
		if ChromeCapabilities.Has(c) {
			t.Fatalf("chrome capability set includes forbidden capability %b", c)
		}
	}
	// And must receive its workflow set.
	for _, c := range []Capability{CapRecordMatch, CapRecordRevealForOrigin, CapRecordCreateLogin} {
		if !ChromeCapabilities.Has(c) {
			t.Fatalf("chrome capability set missing %b", c)
		}
	}
}

func TestRelayCapabilities(t *testing.T) {
	if !RelayCapabilities.Has(CapRelay) {
		t.Fatal("relay missing CapRelay")
	}
	for _, c := range []Capability{CapRecordRead, CapRecordMatch, CapVaultUnlock} {
		if RelayCapabilities.Has(c) {
			t.Fatal("relay has record/vault capability")
		}
	}
}

func TestDefaultCapabilities(t *testing.T) {
	if DefaultCapabilities(KindCLI) != CLICapabilities {
		t.Fatal("cli defaults wrong")
	}
	if DefaultCapabilities(KindChromeExtension) != ChromeCapabilities {
		t.Fatal("extension defaults wrong")
	}
	if DefaultCapabilities(KindChromeNativeHost) != RelayCapabilities {
		t.Fatal("relay defaults wrong")
	}
	if DefaultCapabilities(ClientKind(99)) != 0 {
		t.Fatal("unknown kind must get nothing")
	}
}

func TestSessionEpochInvalidation(t *testing.T) {
	now := time.Now()
	s := &Session{
		ID:           shared.ID{1},
		Capabilities: CLICapabilities,
		CreatedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
		VaultEpoch:   3,
	}
	if !s.ValidAt(now, 3) {
		t.Fatal("session should be valid in its epoch")
	}
	if s.ValidAt(now, 4) {
		t.Fatal("session must die when the epoch changes")
	}
	if s.ValidAt(now.Add(2*time.Hour), 3) {
		t.Fatal("expired session must be invalid")
	}
}

func TestSessionAuthorize(t *testing.T) {
	s := &Session{Capabilities: NewCapabilitySet(CapRecordRead)}
	if err := s.Authorize(CapRecordRead); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(CapVaultDestroy); err != shared.ErrAuthorizationDeny {
		t.Fatal("missing capability not denied")
	}
}

func TestClientApproval(t *testing.T) {
	c := &Client{Status: StatusPending}
	if c.IsApproved() {
		t.Fatal("pending client approved")
	}
	c.Status = StatusApproved
	if !c.IsApproved() {
		t.Fatal("approved client not approved")
	}
	c.Status = StatusRevoked
	if c.IsApproved() {
		t.Fatal("revoked client approved")
	}
}
