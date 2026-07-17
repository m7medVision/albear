package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/command"
)

var (
	key     = bytes.Repeat([]byte{0x5A}, 32)
	vaultID = bytes.Repeat([]byte{0xAA}, 16)
	now     = time.Unix(1_700_000_000, 0)
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlite.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	s := sqlite.NewStore(db)
	seed(t, s)
	return s
}

// seed builds a vault with one envelope, one record and one approved client:
// enough that every branch of the serialization is exercised.
func seed(t *testing.T, s *sqlite.Store) {
	t.Helper()
	ctx := context.Background()
	err := s.Command(ctx, func(c *command.Queries) error {
		if err := c.InsertVault(ctx, command.InsertVaultParams{
			VaultID: vaultID, FormatVersion: 1, ActiveEnvelopeVersion: 1,
			CreatedAtMs: 0, UpdatedAtMs: 0,
		}); err != nil {
			return err
		}
		if err := c.InsertKeyEnvelope(ctx, command.InsertKeyEnvelopeParams{
			EnvelopeVersion: 1, VaultID: vaultID,
			KdfAlgorithm: "argon2id", KdfVersion: 19,
			KdfSalt: bytes.Repeat([]byte{1}, 16), KdfMemoryKib: 65536,
			KdfIterations: 3, KdfParallelism: 1,
			WrapAlgorithm: "xchacha20poly1305",
			WrapNonce:     bytes.Repeat([]byte{2}, 24), WrappedRootKey: []byte("wrapped-v1"),
			CanaryNonce: bytes.Repeat([]byte{3}, 24), EncryptedCanary: []byte("canary"),
			CreatedAtMs: 0,
		}); err != nil {
			return err
		}
		if err := c.InsertRecord(ctx, command.InsertRecordParams{
			RecordID: bytes.Repeat([]byte{0xC1}, 16), KeyVersion: 1, Revision: 1,
			MetadataNonce: bytes.Repeat([]byte{4}, 24), MetadataCiphertext: []byte("meta-1"),
			SecretNonce: bytes.Repeat([]byte{5}, 24), SecretCiphertext: []byte("secret-1"),
			PayloadVersion: 1,
		}); err != nil {
			return err
		}
		return c.InsertClient(ctx, command.InsertClientParams{
			ClientID: bytes.Repeat([]byte{0xE1}, 16), ClientKind: 2,
			Status: 2, CapabilityMask: 7,
			CredentialHash:    bytes.Repeat([]byte{6}, 32),
			NoiseStaticPubkey: bytes.Repeat([]byte{7}, 32),
			LabelNonce:        bytes.Repeat([]byte{8}, 24), LabelCiphertext: []byte("label"),
			CreatedAtMs: 0,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func stamp(t *testing.T, s *sqlite.Store) {
	t.Helper()
	err := s.CommandTx(context.Background(), func(tx *sql.Tx, _ *command.Queries) error {
		return Stamp(context.Background(), tx, key, vaultID, 1, now)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// verify reports whether the stored root still matches the catalog.
func verify(t *testing.T, s *sqlite.Store) bool {
	t.Helper()
	var ok bool
	err := s.Read(context.Background(), func(tx *sql.Tx) error {
		var err error
		_, ok, err = Verify(context.Background(), tx, key, vaultID, 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func exec(t *testing.T, s *sqlite.Store, q string, args ...any) {
	t.Helper()
	if _, err := s.DB().ExecContext(context.Background(), q, args...); err != nil {
		t.Fatal(err)
	}
}

func TestRootIsDeterministic(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	var a, b []byte
	s.Read(ctx, func(tx *sql.Tx) error {
		var err error
		a, err = Root(ctx, tx, key, vaultID, 1, 1)
		return err
	})
	s.Read(ctx, func(tx *sql.Tx) error {
		var err error
		b, err = Root(ctx, tx, key, vaultID, 1, 1)
		return err
	})
	if !bytes.Equal(a, b) {
		t.Fatal("the same catalog hashed differently")
	}
	if len(a) != 32 {
		t.Fatalf("root is %d bytes", len(a))
	}
}

// TestRootBindsItsInputs: each thing folded into the serialization must change
// the output. A field that does not is a field an attacker may edit freely.
func TestRootBindsItsInputs(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	base := func() []byte {
		var r []byte
		s.Read(ctx, func(tx *sql.Tx) error {
			var err error
			r, err = Root(ctx, tx, key, vaultID, 1, 7)
			return err
		})
		return r
	}
	original := base()

	differs := func(name string, got []byte) {
		t.Helper()
		if bytes.Equal(original, got) {
			t.Fatalf("root ignores %s", name)
		}
	}

	// Counter, vault id, and envelope version are direct inputs.
	s.Read(ctx, func(tx *sql.Tx) error {
		r, _ := Root(ctx, tx, key, vaultID, 1, 8)
		differs("state_counter", r)
		r, _ = Root(ctx, tx, bytes.Repeat([]byte{0xBB}, 16), vaultID, 1, 7)
		differs("the key", r)
		r, _ = Root(ctx, tx, key, bytes.Repeat([]byte{0xBB}, 16), 1, 7)
		differs("vault_id", r)
		return nil
	})

	// Every catalog field the design names.
	for _, tc := range []struct {
		name string
		mut  string
	}{
		{"record revision", `UPDATE records SET revision = 2`},
		{"record key_version", `UPDATE records SET key_version = 9`},
		{"record payload_version", `UPDATE records SET payload_version = 9`},
		{"metadata ciphertext", `UPDATE records SET metadata_ciphertext = x'FFFF'`},
		{"secret ciphertext", `UPDATE records SET secret_ciphertext = x'FFFF'`},
		{"client status", `UPDATE clients SET status = 3`},
		{"client capability_mask", `UPDATE clients SET capability_mask = 4095`},
		{"client credential_hash", `UPDATE clients SET credential_hash = zeroblob(32)`},
		{"client static key", `UPDATE clients SET noise_static_pubkey = zeroblob(32)`},
		{"wrapped root key", `UPDATE key_envelopes SET wrapped_root_key = x'FFFF'`},
		{"kdf salt", `UPDATE key_envelopes SET kdf_salt = zeroblob(16)`},
		{"kdf memory", `UPDATE key_envelopes SET kdf_memory_kib = 131072`},
		{"kdf iterations", `UPDATE key_envelopes SET kdf_iterations = 4`},
		{"kdf parallelism", `UPDATE key_envelopes SET kdf_parallelism = 2`},
		{"record deletion", `DELETE FROM records`},
		{"client deletion", `DELETE FROM clients`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := newStore(t)
			var before, after []byte
			fresh.Read(ctx, func(tx *sql.Tx) error {
				var err error
				before, err = Root(ctx, tx, key, vaultID, 1, 7)
				return err
			})
			exec(t, fresh, tc.mut)
			fresh.Read(ctx, func(tx *sql.Tx) error {
				var err error
				after, err = Root(ctx, tx, key, vaultID, 1, 7)
				return err
			})
			if bytes.Equal(before, after) {
				t.Fatalf("root ignores %s", tc.name)
			}
		})
	}
}

// TestRootIgnoresNonSecurityFields: last_seen_at_ms changes on every
// connection. If it were in the root, every handshake would need a stamp and
// the anchor would be worthless churn.
func TestRootIgnoresNonSecurityFields(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	var before, after []byte
	s.Read(ctx, func(tx *sql.Tx) error {
		var err error
		before, err = Root(ctx, tx, key, vaultID, 1, 1)
		return err
	})
	exec(t, s, `UPDATE clients SET last_seen_at_ms = 99999`)
	exec(t, s, `INSERT INTO security_events (occurred_at_ms, severity, event_code) VALUES (1, 1, 100)`)
	s.Read(ctx, func(tx *sql.Tx) error {
		var err error
		after, err = Root(ctx, tx, key, vaultID, 1, 1)
		return err
	})
	if !bytes.Equal(before, after) {
		t.Fatal("root churns on last_seen/security_events")
	}
}

func TestStampAndVerify(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// An unstamped vault has no anchor yet.
	var loadErr error
	s.Read(ctx, func(tx *sql.Tx) error {
		_, loadErr = Load(ctx, tx)
		return nil
	})
	if !errors.Is(loadErr, ErrNoState) {
		t.Fatalf("unstamped vault reported a state: %v", loadErr)
	}

	stamp(t, s)
	if !verify(t, s) {
		t.Fatal("a freshly stamped catalog does not verify")
	}

	var st State
	s.Read(ctx, func(tx *sql.Tx) error {
		var err error
		st, err = Load(ctx, tx)
		return err
	})
	if st.Counter != 1 {
		t.Fatalf("first stamp produced counter %d", st.Counter)
	}

	// The counter advances once per stamp.
	stamp(t, s)
	s.Read(ctx, func(tx *sql.Tx) error {
		var err error
		st, err = Load(ctx, tx)
		return err
	})
	if st.Counter != 2 {
		t.Fatalf("second stamp produced counter %d", st.Counter)
	}
	if !verify(t, s) {
		t.Fatal("re-stamped catalog does not verify")
	}
}

// TestVerifyDetectsTampering walks the attacks the anchor exists to catch.
// Each one leaves every individual ciphertext perfectly valid.
func TestVerifyDetectsTampering(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  string
	}{
		{"a record is deleted", `DELETE FROM records`},
		{"a record is reverted to an older ciphertext", `UPDATE records SET secret_ciphertext = x'DEAD', revision = 1`},
		// The seeded client is approved, so this is the edit in the revoking
		// direction; TestVerifyDetectsRevocationRevert covers the reinstating
		// one, which needs a revoked baseline to mean anything.
		{"a client's status is edited", `UPDATE clients SET status = 3`},
		{"a client's capabilities are widened", `UPDATE clients SET capability_mask = 1048575`},
		{"a client's pinned key is swapped", `UPDATE clients SET noise_static_pubkey = zeroblob(32)`},
		{"an old key envelope is restored", `UPDATE key_envelopes SET wrapped_root_key = x'0102', kdf_salt = zeroblob(16)`},
		{"the root itself is overwritten", `UPDATE vault_state SET state_root = zeroblob(32)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			stamp(t, s)
			if !verify(t, s) {
				t.Fatal("baseline does not verify")
			}
			exec(t, s, tc.mut)
			if verify(t, s) {
				t.Fatalf("undetected: %s", tc.name)
			}
		})
	}
}

// TestVerifyDetectsRevocationRevert is the scenario spelled out end to end: a
// user revokes a stolen client, and the attacker edits the row back.
func TestVerifyDetectsRevocationRevert(t *testing.T) {
	s := newStore(t)

	// Revoke, anchored.
	exec(t, s, `UPDATE clients SET status = 3`)
	stamp(t, s)
	if !verify(t, s) {
		t.Fatal("post-revocation baseline does not verify")
	}

	// The attacker puts it back. Every row is still individually valid.
	exec(t, s, `UPDATE clients SET status = 2`)
	if verify(t, s) {
		t.Fatal("revocation revert went undetected")
	}
}

// TestReplayingAnOldAnchorFails: the counter is inside the HMAC, so an old
// (root, counter) pair cannot be pasted onto a newer catalog.
func TestReplayingAnOldAnchorFails(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	stamp(t, s)
	var old State
	s.Read(ctx, func(tx *sql.Tx) error {
		var err error
		old, err = Load(ctx, tx)
		return err
	})

	// The catalog moves on, anchored.
	exec(t, s, `UPDATE records SET secret_ciphertext = x'AABB', revision = 2`)
	stamp(t, s)
	if !verify(t, s) {
		t.Fatal("legitimate change does not verify")
	}

	// The attacker restores the old anchor to make the old catalog look
	// current — but the catalog is the new one, so it still mismatches.
	exec(t, s, `UPDATE vault_state SET state_counter = ?, state_root = ?`, old.Counter, old.Root)
	if verify(t, s) {
		t.Fatal("an old anchor validated a newer catalog")
	}
}

// TestForgingTheRootNeedsTheKey: the whole guarantee rests on the catalog key
// being a root-key derivative, absent from disk while locked.
func TestForgingTheRootNeedsTheKey(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	stamp(t, s)

	// An attacker who edits rows and re-stamps with a guessed key produces a
	// root that does not verify under the real one.
	attacker := bytes.Repeat([]byte{0x99}, 32)
	exec(t, s, `DELETE FROM records`)
	if err := s.CommandTx(ctx, func(tx *sql.Tx, _ *command.Queries) error {
		return Stamp(ctx, tx, attacker, vaultID, 1, now)
	}); err != nil {
		t.Fatal(err)
	}
	if verify(t, s) {
		t.Fatal("a root forged under the wrong key verified")
	}
}
