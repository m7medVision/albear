package application

import (
	"context"
	"path/filepath"
	"testing"

	"albear/internal/infrastructure/crypto"
	"albear/internal/infrastructure/sqlite"
	domain "albear/internal/security/domain"
	vaultapp "albear/internal/vault/application"
)

var fastParams = crypto.KDFParams{MemoryKiB: crypto.MinMemoryKiB, Iterations: 3, Parallelism: 4}

func newEnv(t *testing.T) (*Recorder, *vaultapp.Service, *sqlite.Store) {
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
	return NewRecorder(store, vs, nil), vs, store
}

func TestRecordWhileLockedDropsDetails(t *testing.T) {
	r, _, store := newEnv(t)
	ctx := context.Background()
	if err := r.Record(ctx, domain.SeverityWarning, domain.EventUnlockFailed, "sensitive detail"); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Query().ListSecurityEvents(ctx, 10)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	if rows[0].DetailsCiphertext != nil {
		t.Fatal("locked-state event stored details")
	}
	if rows[0].EventCode != int64(domain.EventUnlockFailed) {
		t.Fatal("code missing")
	}
}

func TestRecordUnlockedEncryptsDetails(t *testing.T) {
	r, vs, store := newEnv(t)
	ctx := context.Background()
	if err := vs.Unlock(ctx, []byte("master")); err != nil {
		t.Fatal(err)
	}
	if err := r.Record(ctx, domain.SeverityInfo, domain.EventClientApproved, "client chrome@laptop"); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.Query().ListSecurityEvents(ctx, 10)
	if len(rows[0].DetailsCiphertext) == 0 {
		t.Fatal("details not stored")
	}
	// Ciphertext must not contain the plaintext.
	if string(rows[0].DetailsCiphertext) == "client chrome@laptop" {
		t.Fatal("details stored in plaintext")
	}

	// Recent decrypts while unlocked.
	events, err := r.Recent(ctx, 10)
	if err != nil || len(events) != 1 {
		t.Fatal(err)
	}
	if events[0].Details != "client chrome@laptop" {
		t.Fatalf("details %q", events[0].Details)
	}

	// After lock, details become unreadable but codes remain.
	vs.Lock()
	events, err = r.Recent(ctx, 10)
	if err != nil || len(events) != 1 {
		t.Fatal(err)
	}
	if events[0].Details == "client chrome@laptop" {
		t.Fatal("details readable while locked")
	}
	if events[0].Code != domain.EventClientApproved {
		t.Fatal("code lost")
	}
}
