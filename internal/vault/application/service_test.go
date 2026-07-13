package application

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"albear/internal/infrastructure/crypto"
	"albear/internal/infrastructure/sqlite"
	shared "albear/internal/shared/domain"
)

// fastParams meets the schema hard minimums while staying quick in tests.
var fastParams = crypto.KDFParams{MemoryKiB: crypto.MinMemoryKiB, Iterations: 3, Parallelism: 4}

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func newService(t *testing.T) (*Service, *fakeClock) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlite.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	return NewService(sqlite.NewStore(db), clk), clk
}

func pw(s string) []byte { return []byte(s) }

func TestInitUnlockLockCycle(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	st, err := svc.Status(ctx)
	if err != nil || st.Initialized {
		t.Fatalf("fresh status: %+v %v", st, err)
	}

	if err := svc.Init(ctx, pw("master password"), fastParams); err != nil {
		t.Fatal(err)
	}
	if err := svc.Init(ctx, pw("master password"), fastParams); !errors.Is(err, shared.ErrVaultExists) {
		t.Fatalf("double init: %v", err)
	}

	st, _ = svc.Status(ctx)
	if !st.Initialized || st.Unlocked {
		t.Fatalf("post-init status %+v", st)
	}

	// Keys unavailable while locked.
	if _, err := svc.Keys(); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal("keys served while locked")
	}

	if err := svc.Unlock(ctx, pw("master password")); err != nil {
		t.Fatal(err)
	}
	st, _ = svc.Status(ctx)
	if !st.Unlocked {
		t.Fatal("not unlocked")
	}
	kr, err := svc.Keys()
	if err != nil {
		t.Fatal(err)
	}
	if len(kr.Metadata) != 32 || len(kr.Secret) != 32 {
		t.Fatal("keyring incomplete")
	}
	if bytes.Equal(kr.Metadata, kr.Secret) {
		t.Fatal("subkeys not separated")
	}

	epochBefore := svc.Epoch()
	locked := false
	svc.OnLock(func() { locked = true })
	svc.Lock()
	if !locked {
		t.Fatal("lock callback not fired")
	}
	if svc.Epoch() == epochBefore {
		t.Fatal("epoch did not advance on lock")
	}
	if _, err := svc.Keys(); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal("keys served after lock")
	}
}

func TestWrongPasswordGeneric(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if err := svc.Init(ctx, pw("right"), fastParams); err != nil {
		t.Fatal(err)
	}
	err := svc.Unlock(ctx, pw("wrong"))
	if !errors.Is(err, shared.ErrAuthenticationFail) {
		t.Fatalf("want generic auth failure, got %v", err)
	}
}

func TestUnlockRateLimiting(t *testing.T) {
	svc, clk := newService(t)
	ctx := context.Background()
	if err := svc.Init(ctx, pw("right"), fastParams); err != nil {
		t.Fatal(err)
	}

	// First three failures: no delay imposed.
	for i := 0; i < 3; i++ {
		if err := svc.Unlock(ctx, pw("wrong")); !errors.Is(err, shared.ErrAuthenticationFail) {
			t.Fatal(err)
		}
	}
	// Fourth failure sets a delay; an immediate retry is rate limited.
	if err := svc.Unlock(ctx, pw("wrong")); !errors.Is(err, shared.ErrAuthenticationFail) {
		t.Fatal(err)
	}
	if err := svc.Unlock(ctx, pw("right")); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limit, got %v", err)
	}
	// After the delay passes, the correct password unlocks and resets state.
	clk.now = clk.now.Add(time.Minute)
	if err := svc.Unlock(ctx, pw("right")); err != nil {
		t.Fatal(err)
	}
}

func TestPanicLock(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Init(ctx, pw("p"), fastParams)
	svc.Unlock(ctx, pw("p"))
	svc.PanicLock()
	if _, err := svc.Keys(); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal("panic lock left keys available")
	}
}

func TestChangeMasterPassword(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if err := svc.Init(ctx, pw("old password"), fastParams); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unlock(ctx, pw("old password")); err != nil {
		t.Fatal(err)
	}
	oldKeys, _ := svc.Keys()
	oldMeta := bytes.Clone(oldKeys.Metadata)

	// Wrong current password rejected.
	if err := svc.ChangeMasterPassword(ctx, pw("bogus"), pw("new password"), fastParams); !errors.Is(err, shared.ErrAuthenticationFail) {
		t.Fatal(err)
	}

	if err := svc.ChangeMasterPassword(ctx, pw("old password"), pw("new password"), fastParams); err != nil {
		t.Fatal(err)
	}
	// Change must lock the vault.
	if _, err := svc.Keys(); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal("vault not locked after password change")
	}
	// Old password no longer works.
	if err := svc.Unlock(ctx, pw("old password")); !errors.Is(err, shared.ErrAuthenticationFail) {
		t.Fatal("old password still accepted")
	}
	// New password works and derives the SAME subkeys (root key unchanged →
	// records need no re-encryption, PRD 15.6).
	if err := svc.Unlock(ctx, pw("new password")); err != nil {
		t.Fatal(err)
	}
	newKeys, _ := svc.Keys()
	if !bytes.Equal(oldMeta, newKeys.Metadata) {
		t.Fatal("root key changed across password change")
	}
}

func TestVerifyPassword(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Init(ctx, pw("secret"), fastParams)
	if err := svc.VerifyPassword(ctx, pw("secret")); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyPassword(ctx, pw("nope")); !errors.Is(err, shared.ErrAuthenticationFail) {
		t.Fatal(err)
	}
}

func TestVaultInfo(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if _, _, _, err := svc.VaultInfo(); !errors.Is(err, shared.ErrVaultNotFound) {
		t.Fatal("info before init")
	}
	svc.Init(ctx, pw("p"), fastParams)
	id, fv, kv, err := svc.VaultInfo()
	if err != nil || len(id) != 16 || fv != 1 || kv != 1 {
		t.Fatalf("%v %d %d %v", id, fv, kv, err)
	}
}

func TestTamperedEnvelopeFailsGenerically(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Init(ctx, pw("p"), fastParams)

	// Corrupt the wrapped root key directly in the database.
	if _, err := svc.store.DB().Exec(
		`UPDATE key_envelopes SET wrapped_root_key = randomblob(length(wrapped_root_key))`); err != nil {
		t.Fatal(err)
	}
	err := svc.Unlock(ctx, pw("p"))
	// PRD 27.5: invalid password and invalid envelope must be indistinguishable.
	if !errors.Is(err, shared.ErrAuthenticationFail) {
		t.Fatalf("tampered envelope error %v", err)
	}
}

func TestUnlockAfterReopenStartsLocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	ctx := context.Background()

	db, _ := sqlite.Open(path)
	sqlite.Migrate(ctx, db)
	svc := NewService(sqlite.NewStore(db), nil)
	svc.Init(ctx, pw("p"), fastParams)
	svc.Unlock(ctx, pw("p"))
	db.Close()

	// Fresh process: new service over the same file must start locked.
	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	svc2 := NewService(sqlite.NewStore(db2), nil)
	st, err := svc2.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Initialized || st.Unlocked {
		t.Fatalf("restart status %+v", st)
	}
	if err := svc2.Unlock(ctx, pw("p")); err != nil {
		t.Fatal(err)
	}
}
