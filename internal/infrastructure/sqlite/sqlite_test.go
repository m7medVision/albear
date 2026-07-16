package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/command"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/query"
)

func openTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func seedVault(t *testing.T, s *Store) []byte {
	t.Helper()
	ctx := context.Background()
	vaultID := bytes.Repeat([]byte{0xAA}, 16)
	err := s.Command(ctx, func(c *command.Queries) error {
		if err := c.InsertVault(ctx, command.InsertVaultParams{
			VaultID: vaultID, FormatVersion: 1, ActiveEnvelopeVersion: 1,
			CreatedAtMs: time.Now().UnixMilli(), UpdatedAtMs: time.Now().UnixMilli(),
		}); err != nil {
			return err
		}
		return c.InsertKeyEnvelope(ctx, command.InsertKeyEnvelopeParams{
			EnvelopeVersion: 1, VaultID: vaultID,
			KdfAlgorithm: "argon2id", KdfVersion: 19,
			KdfSalt: bytes.Repeat([]byte{1}, 16), KdfMemoryKib: 65536,
			KdfIterations: 3, KdfParallelism: 1,
			WrapAlgorithm: "xchacha20poly1305",
			WrapNonce:     bytes.Repeat([]byte{2}, 24), WrappedRootKey: []byte("wrapped"),
			CanaryNonce: bytes.Repeat([]byte{3}, 24), EncryptedCanary: []byte("canary"),
			CreatedAtMs: time.Now().UnixMilli(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return vaultID
}

func TestMigrateIsIdempotentAndChecksummed(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	v, err := SchemaVersion(ctx, db)
	if err != nil || v != 1 {
		t.Fatalf("schema version %d, err %v", v, err)
	}

	// Tampering with the recorded checksum must make migration refuse.
	if _, err := db.Exec(`UPDATE schema_migrations SET checksum = zeroblob(32)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum mismatch not detected: %v", err)
	}
}

func TestStrictSchemaRejectsBadTypes(t *testing.T) {
	db, _ := openTestDB(t)
	// STRICT table: text in an INTEGER column must fail.
	_, err := db.Exec(`INSERT INTO security_events (occurred_at_ms, severity, event_code) VALUES ('now', 1, 1)`)
	if err == nil {
		t.Fatal("STRICT table accepted a lossy type")
	}
	// CHECK: short vault_id must fail.
	_, err = db.Exec(`INSERT INTO vault (singleton_id, vault_id, format_version, active_envelope_version, created_at_ms, updated_at_ms)
		VALUES (1, x'00', 1, 1, 0, 0)`)
	if err == nil {
		t.Fatal("CHECK constraint on vault_id length not enforced")
	}
}

func TestVaultSingleton(t *testing.T) {
	db, _ := openTestDB(t)
	s := NewStore(db)
	seedVault(t, s)
	ctx := context.Background()
	err := s.Command(ctx, func(c *command.Queries) error {
		return c.InsertVault(ctx, command.InsertVaultParams{
			VaultID: bytes.Repeat([]byte{0xBB}, 16), FormatVersion: 1, ActiveEnvelopeVersion: 1,
		})
	})
	if err == nil {
		t.Fatal("second vault row accepted")
	}
}

func TestRecordCRUDWithOptimisticConcurrency(t *testing.T) {
	db, _ := openTestDB(t)
	s := NewStore(db)
	seedVault(t, s)
	ctx := context.Background()
	recID := bytes.Repeat([]byte{0xCC}, 16)

	if err := s.Command(ctx, func(c *command.Queries) error {
		return c.InsertRecord(ctx, command.InsertRecordParams{
			RecordID: recID, KeyVersion: 1, Revision: 1,
			MetadataNonce: bytes.Repeat([]byte{4}, 24), MetadataCiphertext: []byte("m1"),
			SecretNonce: bytes.Repeat([]byte{5}, 24), SecretCiphertext: []byte("s1"),
			PayloadVersion: 1,
		})
	}); err != nil {
		t.Fatal(err)
	}

	// Duplicate record ID must fail.
	if err := s.Command(ctx, func(c *command.Queries) error {
		return c.InsertRecord(ctx, command.InsertRecordParams{
			RecordID: recID, KeyVersion: 1, Revision: 1,
			MetadataNonce: bytes.Repeat([]byte{6}, 24), MetadataCiphertext: []byte("m"),
			SecretNonce: bytes.Repeat([]byte{7}, 24), SecretCiphertext: []byte("s"),
			PayloadVersion: 1,
		})
	}); err == nil {
		t.Fatal("duplicate record id accepted")
	}

	// Update with correct expected revision succeeds.
	var rows int64
	err := s.Command(ctx, func(c *command.Queries) error {
		var err error
		rows, err = c.UpdateRecord(ctx, command.UpdateRecordParams{
			Revision:      2,
			MetadataNonce: bytes.Repeat([]byte{8}, 24), MetadataCiphertext: []byte("m2"),
			SecretNonce: bytes.Repeat([]byte{9}, 24), SecretCiphertext: []byte("s2"),
			PayloadVersion: 1, RecordID: recID, Revision_2: 1,
		})
		return err
	})
	if err != nil || rows != 1 {
		t.Fatalf("update: rows=%d err=%v", rows, err)
	}

	// Stale revision updates zero rows.
	err = s.Command(ctx, func(c *command.Queries) error {
		var err error
		rows, err = c.UpdateRecord(ctx, command.UpdateRecordParams{
			Revision:      3,
			MetadataNonce: bytes.Repeat([]byte{8}, 24), MetadataCiphertext: []byte("m3"),
			SecretNonce: bytes.Repeat([]byte{9}, 24), SecretCiphertext: []byte("s3"),
			PayloadVersion: 1, RecordID: recID, Revision_2: 1,
		})
		return err
	})
	if err != nil || rows != 0 {
		t.Fatalf("stale update: rows=%d err=%v", rows, err)
	}

	got, err := s.Query().GetRecord(ctx, recID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || string(got.MetadataCiphertext) != "m2" {
		t.Fatalf("read back %+v", got)
	}

	n, err := s.Query().CountRecords(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count %d err %v", n, err)
	}

	err = s.Command(ctx, func(c *command.Queries) error {
		var err error
		rows, err = c.DeleteRecord(ctx, recID)
		return err
	})
	if err != nil || rows != 1 {
		t.Fatalf("delete rows=%d err=%v", rows, err)
	}
	if _, err := s.Query().GetRecord(ctx, recID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted record still readable: %v", err)
	}
}

func TestCommandRollsBackOnError(t *testing.T) {
	db, _ := openTestDB(t)
	s := NewStore(db)
	seedVault(t, s)
	ctx := context.Background()
	recID := bytes.Repeat([]byte{0xDD}, 16)

	sentinel := errors.New("boom")
	err := s.Command(ctx, func(c *command.Queries) error {
		if err := c.InsertRecord(ctx, command.InsertRecordParams{
			RecordID: recID, KeyVersion: 1, Revision: 1,
			MetadataNonce: bytes.Repeat([]byte{4}, 24), MetadataCiphertext: []byte("m"),
			SecretNonce: bytes.Repeat([]byte{5}, 24), SecretCiphertext: []byte("s"),
			PayloadVersion: 1,
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	// The insert inside the failed transaction must not be visible.
	if _, err := s.Query().GetRecord(ctx, recID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("rolled-back insert visible")
	}
}

func TestClientLifecycle(t *testing.T) {
	db, _ := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	clientID := bytes.Repeat([]byte{0xEE}, 16)

	if err := s.Command(ctx, func(c *command.Queries) error {
		return c.InsertClient(ctx, command.InsertClientParams{
			ClientID: clientID, ClientKind: 1, Status: 1, CapabilityMask: 7,
			CredentialHash:    bytes.Repeat([]byte{1}, 32),
			NoiseStaticPubkey: bytes.Repeat([]byte{2}, 32),
			LabelNonce:        bytes.Repeat([]byte{3}, 24), LabelCiphertext: []byte("l"),
			CreatedAtMs: time.Now().UnixMilli(),
		})
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Query().GetClient(ctx, clientID)
	if err != nil || got.Status != 1 {
		t.Fatalf("%+v %v", got, err)
	}

	var rows int64
	s.Command(ctx, func(c *command.Queries) error {
		rows, err = c.UpdateClientStatus(ctx, command.UpdateClientStatusParams{Status: 2, ClientID: clientID})
		return err
	})
	if rows != 1 {
		t.Fatal("status update missed")
	}

	list, err := s.Query().ListClients(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %d %v", len(list), err)
	}

	s.Command(ctx, func(c *command.Queries) error {
		rows, err = c.DeleteClient(ctx, clientID)
		return err
	})
	if rows != 1 {
		t.Fatal("delete missed")
	}
}

func TestSecurityEvents(t *testing.T) {
	db, _ := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := s.Command(ctx, func(c *command.Queries) error {
			return c.InsertSecurityEvent(ctx, command.InsertSecurityEventParams{
				OccurredAtMs: int64(i), Severity: 1, EventCode: int64(100 + i),
			})
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.Query().ListSecurityEvents(ctx, 2)
	if err != nil || len(events) != 2 {
		t.Fatalf("%d %v", len(events), err)
	}
	// Newest first.
	if events[0].EventCode != 102 {
		t.Fatalf("order wrong: %+v", events[0])
	}
}

func TestSnapshotProducesConsistentCopy(t *testing.T) {
	db, _ := openTestDB(t)
	s := NewStore(db)
	seedVault(t, s)
	ctx := context.Background()

	snap := filepath.Join(t.TempDir(), "snap.db")
	if err := s.Snapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snap); err != nil {
		t.Fatal(err)
	}

	sdb, err := Open(snap)
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	v, err := query.New(sdb).GetVault(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.VaultID) != 16 {
		t.Fatal("snapshot missing vault row")
	}
}

func TestKeyEnvelopeSwap(t *testing.T) {
	db, _ := openTestDB(t)
	s := NewStore(db)
	vaultID := seedVault(t, s)
	ctx := context.Background()

	// Atomic envelope replacement: insert v2, activate, delete v1.
	err := s.Command(ctx, func(c *command.Queries) error {
		if err := c.InsertKeyEnvelope(ctx, command.InsertKeyEnvelopeParams{
			EnvelopeVersion: 2, VaultID: vaultID,
			KdfAlgorithm: "argon2id", KdfVersion: 19,
			KdfSalt: bytes.Repeat([]byte{9}, 16), KdfMemoryKib: 65536,
			KdfIterations: 3, KdfParallelism: 1,
			WrapAlgorithm: "xchacha20poly1305",
			WrapNonce:     bytes.Repeat([]byte{8}, 24), WrappedRootKey: []byte("wrapped2"),
			CanaryNonce: bytes.Repeat([]byte{7}, 24), EncryptedCanary: []byte("canary2"),
			CreatedAtMs: time.Now().UnixMilli(),
		}); err != nil {
			return err
		}
		if err := c.SetActiveEnvelope(ctx, command.SetActiveEnvelopeParams{
			ActiveEnvelopeVersion: 2, UpdatedAtMs: time.Now().UnixMilli(),
		}); err != nil {
			return err
		}
		return c.DeleteKeyEnvelope(ctx, 1)
	})
	if err != nil {
		t.Fatal(err)
	}

	v, err := s.Query().GetVault(ctx)
	if err != nil || v.ActiveEnvelopeVersion != 2 {
		t.Fatalf("%+v %v", v, err)
	}
	versions, err := s.Query().ListKeyEnvelopeVersions(ctx)
	if err != nil || len(versions) != 1 || versions[0] != 2 {
		t.Fatalf("%v %v", versions, err)
	}
	if _, err := s.Query().GetKeyEnvelope(ctx, 2); err != nil {
		t.Fatal(err)
	}
}

func TestOpenBadPath(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "missing", "sub", "vault.db")); err == nil {
		t.Fatal("open into missing directory succeeded")
	}
}
