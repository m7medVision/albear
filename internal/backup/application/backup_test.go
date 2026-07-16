package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	shared "github.com/m7medVision/albear/internal/shared/domain"
	vaultapp "github.com/m7medVision/albear/internal/vault/application"
)

var fastParams = crypto.KDFParams{MemoryKiB: crypto.MinMemoryKiB, Iterations: 3, Parallelism: 4}

func newEnv(t *testing.T) (*Service, *vaultapp.Service, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vault.db")
	db, err := sqlite.Open(dbPath)
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
	return NewService(store, vs, nil), vs, dbPath
}

func TestBackupCreateVerify(t *testing.T) {
	bs, _, _ := newEnv(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "b.abk")

	if err := bs.Create(ctx, path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %v", st.Mode())
	}

	info, err := bs.Verify(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.SnapshotLen == 0 || info.CreatedAtMs == 0 {
		t.Fatalf("%+v", info)
	}

	// Keyless format check also passes.
	if _, err := VerifyContainerFormat(path); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRequiresUnlock(t *testing.T) {
	bs, vs, _ := newEnv(t)
	vs.Lock()
	if err := bs.Create(context.Background(), filepath.Join(t.TempDir(), "b.abk")); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal(err)
	}
}

func TestBackupTamperDetected(t *testing.T) {
	bs, _, _ := newEnv(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "b.abk")
	bs.Create(ctx, path)

	raw, _ := os.ReadFile(path)
	raw[len(raw)/2] ^= 1
	os.WriteFile(path, raw, 0o600)

	if _, err := bs.Verify(path); !errors.Is(err, ErrBadContainer) {
		t.Fatalf("tampered backup verified: %v", err)
	}
}

func TestTruncatedBackupRejected(t *testing.T) {
	bs, _, _ := newEnv(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "b.abk")
	bs.Create(ctx, path)

	raw, _ := os.ReadFile(path)
	os.WriteFile(path, raw[:len(raw)-40], 0o600)
	if _, err := VerifyContainerFormat(path); !errors.Is(err, ErrBadContainer) {
		t.Fatal("truncated container accepted")
	}

	os.WriteFile(path, []byte("junk"), 0o600)
	if _, err := VerifyContainerFormat(path); !errors.Is(err, ErrBadContainer) {
		t.Fatal("junk accepted")
	}
}

func TestBackupContainsNoPlaintext(t *testing.T) {
	// A record's secrets must not appear in the backup bytes.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vault.db")
	db, _ := sqlite.Open(dbPath)
	defer db.Close()
	ctx := context.Background()
	sqlite.Migrate(ctx, db)
	store := sqlite.NewStore(db)
	vs := vaultapp.NewService(store, nil)
	// Distinctive needle: "master" alone would match SQLite's internal
	// sqlite_master table name in the snapshot.
	vs.Init(ctx, []byte("xkcd936-correct-horse"), fastParams)
	vs.Unlock(ctx, []byte("xkcd936-correct-horse"))
	bs := NewService(store, vs, nil)

	// Insert a record through raw crypto path is heavy; the vault + envelope
	// alone must already contain no key material. Check the backup for the
	// master password and canary plaintext.
	path := filepath.Join(dir, "b.abk")
	if err := bs.Create(ctx, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	for _, needle := range []string{"xkcd936-correct-horse", "albear-canary-v1"} {
		if strings.Contains(string(raw), needle) {
			t.Fatalf("backup contains plaintext %q", needle)
		}
	}
}

func TestRestoreReplacesDatabase(t *testing.T) {
	bs, _, dbPath := newEnv(t)
	ctx := context.Background()
	backupPath := filepath.Join(t.TempDir(), "b.abk")
	if err := bs.Create(ctx, backupPath); err != nil {
		t.Fatal(err)
	}

	newDBPath := filepath.Join(t.TempDir(), "restored.db")
	verified := false
	err := Restore(backupPath, newDBPath, func(candidate string) error {
		verified = true
		db, err := sqlite.Open(candidate)
		if err != nil {
			return err
		}
		defer db.Close()
		_, err = sqlite.NewStore(db).Query().GetVault(context.Background())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("verify callback not called")
	}

	// Restored database opens and contains the vault.
	db, err := sqlite.Open(newDBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := sqlite.NewStore(db).Query().GetVault(ctx)
	if err != nil || len(v.VaultID) != 16 {
		t.Fatal(err)
	}
	_ = dbPath
}

func TestRestoreKeepsRecoveryCopy(t *testing.T) {
	bs, _, _ := newEnv(t)
	ctx := context.Background()
	backupPath := filepath.Join(t.TempDir(), "b.abk")
	bs.Create(ctx, backupPath)

	dir := t.TempDir()
	target := filepath.Join(dir, "vault.db")
	os.WriteFile(target, []byte("old database"), 0o600)

	if err := Restore(backupPath, target, nil); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(target + ".recovery")
	if err != nil || string(old) != "old database" {
		t.Fatal("recovery copy missing")
	}
}

func TestRestoreFailsClosedOnBadVerify(t *testing.T) {
	bs, _, _ := newEnv(t)
	ctx := context.Background()
	backupPath := filepath.Join(t.TempDir(), "b.abk")
	bs.Create(ctx, backupPath)

	dir := t.TempDir()
	target := filepath.Join(dir, "vault.db")
	os.WriteFile(target, []byte("old database"), 0o600)

	err := Restore(backupPath, target, func(string) error { return errors.New("bad") })
	if err == nil {
		t.Fatal("restore proceeded despite failed verification")
	}
	// Original database untouched.
	cur, _ := os.ReadFile(target)
	if string(cur) != "old database" {
		t.Fatal("original database replaced on failed restore")
	}
}
