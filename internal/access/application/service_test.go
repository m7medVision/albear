package application

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	domain "github.com/m7medVision/albear/internal/access/domain"
	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	shared "github.com/m7medVision/albear/internal/shared/domain"
	vaultapp "github.com/m7medVision/albear/internal/vault/application"
)

var fastParams = crypto.KDFParams{MemoryKiB: crypto.MinMemoryKiB, Iterations: 3, Parallelism: 4}

func newEnv(t *testing.T) (*Service, *vaultapp.Service) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewStore(db)
	vs := vaultapp.NewService(store, nil)
	if err := vs.Init(ctx, []byte("master"), fastParams); err != nil {
		t.Fatal(err)
	}
	if err := vs.Unlock(ctx, []byte("master")); err != nil {
		t.Fatal(err)
	}
	cs := NewService(store, vs, nil)
	cs.SetDaemonStaticKey(bytes.Repeat([]byte{7}, 32))
	return cs, vs
}

func staticKey(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func TestPairingLifecycle(t *testing.T) {
	cs, _ := newEnv(t)
	ctx := context.Background()

	p, err := cs.RequestPairing(domain.KindChromeExtension, "chrome@laptop", staticKey(1))
	if err != nil {
		t.Fatal(err)
	}
	if p.Phrase == "" || len(p.Phrase) != 14 {
		t.Fatalf("phrase %q", p.Phrase)
	}

	if got := cs.ListPending(); len(got) != 1 || got[0].ID != p.ID {
		t.Fatal("pending list wrong")
	}

	// Claim before approval is denied.
	if _, _, err := cs.ClaimCredential(p.ID); !errors.Is(err, shared.ErrAuthorizationDeny) {
		t.Fatal("claim before approval")
	}

	if err := cs.Approve(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	clientID, credential, err := cs.ClaimCredential(p.ID)
	if err != nil || len(credential) != 32 {
		t.Fatal(err)
	}

	// Credential claimable exactly once.
	if _, _, err := cs.ClaimCredential(p.ID); !errors.Is(err, shared.ErrClientNotFound) {
		t.Fatal("credential claimable twice")
	}

	// Stored client: verifier matches, static key pinned, capabilities restricted.
	c, err := cs.Lookup(ctx, clientID)
	if err != nil {
		t.Fatal(err)
	}
	if !crypto.VerifierEqual(c.CredentialHash, crypto.CredentialVerifier(credential)) {
		t.Fatal("verifier mismatch")
	}
	if !bytes.Equal(c.StaticKey, staticKey(1)) {
		t.Fatal("static key not pinned")
	}
	if c.Capabilities != domain.ChromeCapabilities {
		t.Fatal("wrong capabilities")
	}
	if !c.IsApproved() {
		t.Fatal("not approved")
	}

	// Label decrypts in listing.
	list, err := cs.List(ctx)
	if err != nil || len(list) != 1 || list[0].Label != "chrome@laptop" {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestPairingPhraseCommitsToKeys(t *testing.T) {
	cs, _ := newEnv(t)
	p1, _ := cs.RequestPairing(domain.KindChromeExtension, "a", staticKey(1))
	p2, _ := cs.RequestPairing(domain.KindChromeExtension, "a", staticKey(2))
	if p1.Phrase == p2.Phrase {
		t.Fatal("different client keys produced identical phrases")
	}
}

func TestApproveRequiresUnlockedVault(t *testing.T) {
	cs, vs := newEnv(t)
	ctx := context.Background()
	p, _ := cs.RequestPairing(domain.KindCLI, "cli", staticKey(3))
	vs.Lock()
	if err := cs.Approve(ctx, p.ID); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatalf("approve while locked: %v", err)
	}
}

func TestApproveUnknownPairing(t *testing.T) {
	cs, _ := newEnv(t)
	if err := cs.Approve(context.Background(), shared.ID{9}); !errors.Is(err, shared.ErrClientNotFound) {
		t.Fatal(err)
	}
}

func TestCancelRemovesPending(t *testing.T) {
	cs, _ := newEnv(t)
	p, err := cs.RequestPairing(domain.KindChromeExtension, "c", staticKey(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.Cancel(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := cs.ListPending(); len(got) != 0 {
		t.Fatalf("pending not removed: %+v", got)
	}
	if err := cs.Cancel(p.ID); !errors.Is(err, shared.ErrClientNotFound) {
		t.Fatalf("second cancel: %v", err)
	}
}

func TestCancelApprovedRejected(t *testing.T) {
	cs, _ := newEnv(t)
	ctx := context.Background()
	p, _ := cs.RequestPairing(domain.KindChromeExtension, "c", staticKey(2))
	if err := cs.Approve(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := cs.Cancel(p.ID); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("cancel after approve: %v", err)
	}
	clientID, cred, err := cs.ClaimCredential(p.ID)
	if err != nil || len(cred) != 32 {
		t.Fatalf("claim after cancel: %v", err)
	}
	c, _ := cs.Lookup(ctx, clientID)
	if !c.IsApproved() {
		t.Fatal("client lost after rejected cancel")
	}
}

func TestRequestPairingValidation(t *testing.T) {
	cs, _ := newEnv(t)
	if _, err := cs.RequestPairing(domain.KindCLI, "x", []byte("short")); !errors.Is(err, shared.ErrValidation) {
		t.Fatal("short static key accepted")
	}
	if _, err := cs.RequestPairing(domain.ClientKind(99), "x", staticKey(1)); !errors.Is(err, shared.ErrValidation) {
		t.Fatal("unknown kind accepted")
	}
}

func TestRevoke(t *testing.T) {
	cs, _ := newEnv(t)
	ctx := context.Background()
	p, _ := cs.RequestPairing(domain.KindChromeExtension, "c", staticKey(4))
	cs.Approve(ctx, p.ID)
	clientID, _, _ := cs.ClaimCredential(p.ID)

	if err := cs.Revoke(ctx, clientID); err != nil {
		t.Fatal(err)
	}
	c, _ := cs.Lookup(ctx, clientID)
	if c.IsApproved() {
		t.Fatal("revoked client still approved")
	}
	if err := cs.Revoke(ctx, shared.ID{1}); !errors.Is(err, shared.ErrClientNotFound) {
		t.Fatal("revoking missing client")
	}
}

func TestLookupMissing(t *testing.T) {
	cs, _ := newEnv(t)
	if _, err := cs.Lookup(context.Background(), shared.ID{1}); !errors.Is(err, shared.ErrClientNotFound) {
		t.Fatal(err)
	}
}

func TestTouchLastSeen(t *testing.T) {
	cs, _ := newEnv(t)
	ctx := context.Background()
	p, _ := cs.RequestPairing(domain.KindCLI, "cli", staticKey(5))
	cs.Approve(ctx, p.ID)
	clientID, _, _ := cs.ClaimCredential(p.ID)
	if err := cs.TouchLastSeen(ctx, clientID); err != nil {
		t.Fatal(err)
	}
	list, _ := cs.List(ctx)
	if list[0].LastSeen.IsZero() {
		t.Fatal("last seen not set")
	}
}

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func TestSessionManager(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	m := NewSessionManager(clk)

	s, err := m.Issue(shared.ID{1}, domain.CLICapabilities, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.Validate(s.ID, 5, true); err != nil || got.ID != s.ID {
		t.Fatal(err)
	}
	// Wrong epoch invalidates immediately and permanently.
	if _, err := m.Validate(s.ID, 6, true); !errors.Is(err, shared.ErrAuthorizationDeny) {
		t.Fatal("stale-epoch session validated")
	}
	if _, err := m.Validate(s.ID, 5, true); !errors.Is(err, shared.ErrAuthorizationDeny) {
		t.Fatal("session resurrected after epoch invalidation")
	}

	// Expiry.
	s2, _ := m.Issue(shared.ID{2}, domain.CLICapabilities, 5)
	clk.now = clk.now.Add(DefaultSessionTTL + time.Minute)
	if _, err := m.Validate(s2.ID, 5, true); !errors.Is(err, shared.ErrAuthorizationDeny) {
		t.Fatal("expired session validated")
	}

	// DropClient and InvalidateAll.
	clk.now = time.Unix(1_700_000_000, 0)
	s3, _ := m.Issue(shared.ID{3}, domain.CLICapabilities, 5)
	s4, _ := m.Issue(shared.ID{3}, domain.CLICapabilities, 5)
	s5, _ := m.Issue(shared.ID{4}, domain.CLICapabilities, 5)
	m.DropClient(shared.ID{3})
	if _, err := m.Validate(s3.ID, 5, true); err == nil {
		t.Fatal("dropped client session alive")
	}
	if _, err := m.Validate(s4.ID, 5, true); err == nil {
		t.Fatal("dropped client session alive")
	}
	if _, err := m.Validate(s5.ID, 5, true); err != nil {
		t.Fatal("unrelated session dropped")
	}
	if m.Count() != 1 {
		t.Fatalf("count %d", m.Count())
	}
	m.InvalidateAll()
	if m.Count() != 0 {
		t.Fatal("invalidate all incomplete")
	}
	m.Drop(s5.ID) // no-op on missing
}

func TestSessionLastActivity(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	m := NewSessionManager(clk)

	if _, ok := m.LastActivity(); ok {
		t.Fatal("empty manager reported activity")
	}

	start := clk.now
	s, _ := m.Issue(shared.ID{1}, domain.CLICapabilities, 5)
	if last, ok := m.LastActivity(); !ok || !last.Equal(start) {
		t.Fatalf("issue did not seed activity: %v %v", last, ok)
	}

	// A non-activity request must not extend the deadline.
	clk.now = start.Add(time.Minute)
	if _, err := m.Validate(s.ID, 5, false); err != nil {
		t.Fatal(err)
	}
	if last, _ := m.LastActivity(); !last.Equal(start) {
		t.Fatalf("polling extended the idle deadline: %v", last)
	}

	// A capability-using request does.
	if _, err := m.Validate(s.ID, 5, true); err != nil {
		t.Fatal(err)
	}
	if last, _ := m.LastActivity(); !last.Equal(clk.now) {
		t.Fatalf("activity not recorded: %v", last)
	}

	// The newest session across the set wins.
	busy := clk.now
	clk.now = busy.Add(time.Minute)
	m.Issue(shared.ID{2}, domain.CLICapabilities, 5)
	if last, _ := m.LastActivity(); !last.Equal(clk.now) {
		t.Fatalf("max across sessions: %v", last)
	}

	// Expired sessions do not hold the deadline open.
	clk.now = clk.now.Add(DefaultSessionTTL + time.Minute)
	if _, ok := m.LastActivity(); ok {
		t.Fatal("expired sessions reported activity")
	}
}
