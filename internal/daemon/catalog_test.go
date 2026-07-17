//go:build linux

package daemon

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/m7medVision/albear/internal/adapters/protocol"
	"github.com/m7medVision/albear/internal/catalog"
	secdomain "github.com/m7medVision/albear/internal/security/domain"
)

// state reads the stored anchor.
func state(t *testing.T, d *testDaemon) catalog.State {
	t.Helper()
	var st catalog.State
	err := d.server.store.Read(context.Background(), func(tx *sql.Tx) error {
		var err error
		st, err = catalog.Load(context.Background(), tx)
		return err
	})
	if err != nil {
		t.Fatalf("load vault state: %v", err)
	}
	return st
}

// TestEveryMutatingOpRestamps is the test the design calls for by name, and
// the one that matters most here. The risk with an anchor over the whole
// catalog is not that an attacker slips past it — it is that *we* forget to
// re-stamp on some write, leaving a stale root that panic-locks an untouched
// vault on the next unlock. So: enumerate every mutating operation, and assert
// each one advances the counter and leaves the catalog verifying.
//
// A new mutating operation that does not stamp will fail here. If you are
// reading this because it did: stamp inside the mutation's transaction (see
// internal/catalog), do not delete the case.
func TestEveryMutatingOpRestamps(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)

	// Init stamps, so a vault from this version is anchored from birth and
	// never needs the trust-on-first-use path.
	if c := state(t, d).Counter; c < 1 {
		t.Fatalf("init did not stamp: counter %d", c)
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := cli.Call("records.create", map[string]any{
		"name": "Seed", "username": "mo", "password": "pw",
		"urls": []string{"https://seed.example"},
	}, &created); err != nil {
		t.Fatal(err)
	}

	// Each step is a mutating operation and the call that performs it.
	steps := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"records.create", func(t *testing.T) {
			var out struct {
				ID string `json:"id"`
			}
			if err := cli.Call("records.create", map[string]any{
				"name": "Made", "username": "mo", "password": "pw",
				"urls": []string{"https://made.example"},
			}, &out); err != nil {
				t.Fatal(err)
			}
		}},
		{"records.update", func(t *testing.T) {
			if err := cli.Call("records.update", map[string]any{
				"id": created.ID, "expectedRevision": 1,
				"name": "Seed", "username": "mo2", "password": "pw2",
				"urls": []string{"https://seed.example"},
			}, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{"records.delete", func(t *testing.T) {
			if err := cli.Call("records.delete", map[string]string{"id": created.ID}, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{"clients.approve", func(t *testing.T) {
			pairExtension(t, d, cli)
		}},
		{"clients.revoke", func(t *testing.T) {
			var list struct {
				Clients []struct {
					ID string `json:"id"`
				} `json:"clients"`
			}
			if err := cli.Call("clients.list", nil, &list); err != nil || len(list.Clients) == 0 {
				t.Fatalf("no client to revoke: %v", err)
			}
			if err := cli.Call("clients.revoke", map[string]string{"id": list.Clients[0].ID}, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{"vault.changePassword", func(t *testing.T) {
			if err := cli.Call("vault.changePassword", map[string]string{
				"current": "master password", "next": "another long passphrase",
			}, nil); err != nil {
				t.Fatal(err)
			}
			// The change locks the vault; unlock again for the next step.
			if err := cli.Call("vault.unlock", map[string]string{"password": "another long passphrase"}, nil); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			before := state(t, d)
			step.run(t)
			after := state(t, d)

			if after.Counter <= before.Counter {
				t.Fatalf("%s did not advance the state counter (%d -> %d)",
					step.name, before.Counter, after.Counter)
			}
			// And the new root must actually describe the new catalog: a
			// counter bump with a stale root would fail closed on next unlock.
			if !verifyState(t, d) {
				t.Fatalf("%s left a root that does not match the catalog", step.name)
			}
		})
	}
}

// verifyState recomputes the root against the live catalog.
func verifyState(t *testing.T, d *testDaemon) bool {
	t.Helper()
	kr, err := d.server.vault.Keys()
	if err != nil {
		t.Fatalf("vault locked: %v", err)
	}
	vaultID, _, envVersion, err := d.server.vault.VaultInfo()
	if err != nil {
		t.Fatal(err)
	}
	var ok bool
	err = d.server.store.Read(context.Background(), func(tx *sql.Tx) error {
		var verr error
		_, ok, verr = catalog.Verify(context.Background(), tx, kr.Catalog, vaultID, envVersion)
		return verr
	})
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// TestNoOpsDoNotStamp: a rejected write changed nothing, so burning a counter
// on it would be noise. This is a correctness detail, not a security one, but
// a counter that moves without a state change makes the log harder to trust.
func TestNoOpsDoNotStamp(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)

	var created struct {
		ID string `json:"id"`
	}
	cli.Call("records.create", map[string]any{
		"name": "N", "username": "mo", "password": "pw", "urls": []string{"https://n.example"},
	}, &created)

	before := state(t, d)

	// A revision conflict updates nothing.
	err := cli.Call("records.update", map[string]any{
		"id": created.ID, "expectedRevision": 99,
		"name": "N", "username": "mo", "password": "pw",
	}, nil)
	if apiCode(t, err) != protocol.CodeConflict {
		t.Fatalf("expected a conflict: %v", err)
	}
	// Deleting something that is not there changes nothing.
	cli.Call("records.delete", map[string]string{"id": "ffffffffffffffffffffffffffffffff"}, nil)
	// Reads change nothing.
	cli.Call("records.list", nil, nil)
	cli.Call("vault.status", nil, nil)

	if after := state(t, d); after.Counter != before.Counter {
		t.Fatalf("a no-op advanced the counter (%d -> %d)", before.Counter, after.Counter)
	}
	if !verifyState(t, d) {
		t.Fatal("no-ops disturbed the root")
	}
}

// TestUnlockDetectsTampering: the whole point. Each of these leaves every
// individual ciphertext valid, so only the catalog root notices. The response
// is to panic-lock and record — never to destroy (invariant 7).
func TestUnlockDetectsTampering(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  string
	}{
		{"a record is deleted", `DELETE FROM records`},
		{"a client is reinstated", `UPDATE clients SET status = 2`},
		{"the state root is overwritten", `UPDATE vault_state SET state_root = zeroblob(32)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := startDaemon(t)
			cli := cliConn(t, d)
			initAndUnlock(t, cli)
			cli.Call("records.create", map[string]any{
				"name": "Victim", "username": "mo", "password": "pw",
				"urls": []string{"https://victim.example"},
			}, nil)
			pairExtension(t, d, cli)
			var list struct {
				Clients []struct {
					ID string `json:"id"`
				} `json:"clients"`
			}
			cli.Call("clients.list", nil, &list)
			if len(list.Clients) > 0 {
				cli.Call("clients.revoke", map[string]string{"id": list.Clients[0].ID}, nil)
			}
			cli.Call("vault.lock", nil, nil)

			// The attacker edits the file directly, as a same-UID process can.
			if _, err := d.server.store.DB().Exec(tc.mut); err != nil {
				t.Fatal(err)
			}

			// The password is still right, so unlock itself succeeds — and
			// then verification fails closed.
			c2 := cliConn(t, d)
			err := c2.Call("vault.unlock", map[string]string{"password": "master password"}, nil)
			if err == nil {
				t.Fatalf("tampering undetected: %s", tc.name)
			}
			if code := apiCode(t, err); code != protocol.CodeIntegrity {
				t.Fatalf("%s: got %s, want %s", tc.name, code, protocol.CodeIntegrity)
			}

			// Locked, not destroyed.
			var status struct {
				Initialized bool `json:"initialized"`
				Unlocked    bool `json:"unlocked"`
			}
			if err := c2.Call("vault.status", nil, &status); err != nil {
				t.Fatal(err)
			}
			if status.Unlocked {
				t.Fatal("vault left unlocked after an integrity failure")
			}
			if !status.Initialized {
				t.Fatal("integrity failure destroyed the vault (invariant 7)")
			}
		})
	}
}

// TestIntegrityFailureIsRecorded: the operator has to be able to find out this
// happened.
func TestIntegrityFailureIsRecorded(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)
	cli.Call("records.create", map[string]any{
		"name": "V", "username": "mo", "password": "pw", "urls": []string{"https://v.example"},
	}, nil)
	cli.Call("vault.lock", nil, nil)

	if _, err := d.server.store.DB().Exec(`DELETE FROM records`); err != nil {
		t.Fatal(err)
	}
	c2 := cliConn(t, d)
	if err := c2.Call("vault.unlock", map[string]string{"password": "master password"}, nil); err == nil {
		t.Fatal("tampering undetected")
	}

	// The event log is written under the audit key, so read it back through a
	// legitimately unlocked vault. Restore the catalog first by re-stamping
	// via a real mutation is not possible while locked — so read events with a
	// direct query instead: the code is what matters, not the details.
	var count int
	row := d.server.store.DB().QueryRow(
		`SELECT count(*) FROM security_events WHERE event_code = ?`,
		int(secdomain.EventIntegrityFailure))
	if err := row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("integrity failure was not recorded")
	}
}

// TestLegacyVaultBootstrapsOnFirstUnlock: a vault created before the state
// table has no anchor. It must adopt the current state rather than refuse to
// open — an existing user upgrading is not an attack.
func TestLegacyVaultBootstrapsOnFirstUnlock(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)
	cli.Call("records.create", map[string]any{
		"name": "Old", "username": "mo", "password": "pw", "urls": []string{"https://old.example"},
	}, nil)
	cli.Call("vault.lock", nil, nil)

	// Stand in for a vault that predates the anchor.
	if _, err := d.server.store.DB().Exec(`DELETE FROM vault_state`); err != nil {
		t.Fatal(err)
	}

	c2 := cliConn(t, d)
	if err := c2.Call("vault.unlock", map[string]string{"password": "master password"}, nil); err != nil {
		t.Fatalf("legacy vault refused to unlock: %v", err)
	}
	// It adopted the current state...
	if st := state(t, d); st.Counter < 1 {
		t.Fatalf("bootstrap did not write an anchor: %d", st.Counter)
	}
	if !verifyState(t, d) {
		t.Fatal("bootstrap wrote a root that does not verify")
	}
	// ...said so...
	var count int
	row := d.server.store.DB().QueryRow(
		`SELECT count(*) FROM security_events WHERE event_code = ?`,
		int(secdomain.EventVaultStateBootstrapped))
	if err := row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("bootstrap was not recorded")
	}
	// ...and the vault works.
	var list struct {
		Records []map[string]any `json:"records"`
	}
	if err := c2.Call("records.list", nil, &list); err != nil || len(list.Records) != 1 {
		t.Fatalf("records lost: %d %v", len(list.Records), err)
	}

	// From here on it is anchored: tampering is caught.
	c2.Call("vault.lock", nil, nil)
	if _, err := d.server.store.DB().Exec(`DELETE FROM records`); err != nil {
		t.Fatal(err)
	}
	c3 := cliConn(t, d)
	if err := c3.Call("vault.unlock", map[string]string{"password": "master password"}, nil); err == nil {
		t.Fatal("bootstrapped vault does not detect later tampering")
	}
}

// TestInRunRollbackDetected: within one daemon run the counter cannot go
// backwards. Across a restart it can — the in-memory high-water mark is the
// only thing that remembers, which is the stated limit of an in-database
// anchor (see internal/catalog and docs/SECURITY.md).
func TestInRunRollbackDetected(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)

	// Move the catalog forward and snapshot the whole anchor at that point.
	cli.Call("records.create", map[string]any{
		"name": "A", "username": "mo", "password": "pw", "urls": []string{"https://a.example"},
	}, nil)
	old := state(t, d)

	cli.Call("records.create", map[string]any{
		"name": "B", "username": "mo", "password": "pw", "urls": []string{"https://b.example"},
	}, nil)
	if state(t, d).Counter <= old.Counter {
		t.Fatal("counter did not advance")
	}
	cli.Call("vault.lock", nil, nil)

	// Roll the catalog back to its earlier state, anchor and all: the second
	// record is gone and the old (counter, root) pair is restored, so the root
	// verifies. Only the counter going backwards gives it away.
	if _, err := d.server.store.DB().Exec(
		`DELETE FROM records WHERE record_id NOT IN (SELECT record_id FROM records ORDER BY record_id LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.server.store.DB().Exec(
		`UPDATE vault_state SET state_counter = ?, state_root = ?`, old.Counter, old.Root); err != nil {
		t.Fatal(err)
	}

	c2 := cliConn(t, d)
	err := c2.Call("vault.unlock", map[string]string{"password": "master password"}, nil)
	if err == nil {
		t.Fatal("in-run rollback undetected")
	}
	if code := apiCode(t, err); code != protocol.CodeIntegrity {
		t.Fatalf("got %s, want %s", code, protocol.CodeIntegrity)
	}
}

// TestBackupRestoreRoundTripVerifies: state_root travels inside the VACUUM
// INTO snapshot, so a restored backup is self-consistent and must unlock
// cleanly. If it did not, every restore would look like an attack.
func TestBackupRestoreRoundTripVerifies(t *testing.T) {
	d := startDaemon(t)
	c := cliConn(t, d)
	initAndUnlock(t, c)
	c.Call("records.create", map[string]any{
		"name": "keepme", "type": "note", "notes": "precious",
	}, nil)

	path := filepath.Join(t.TempDir(), "b.abk")
	if err := c.Call("backup.create", map[string]string{"path": path}, nil); err != nil {
		t.Fatal(err)
	}

	// Move on, then restore.
	c.Call("records.create", map[string]any{"name": "extra", "type": "note", "notes": "x"}, nil)
	if err := c.Call("backup.restore", map[string]string{"path": path}, nil); err != nil {
		t.Fatal(err)
	}

	c2 := cliConn(t, d)
	if err := c2.Call("vault.unlock", map[string]string{"password": "master password"}, nil); err != nil {
		t.Fatalf("restored vault failed its integrity check: %v", err)
	}
	var list struct {
		Records []map[string]any `json:"records"`
	}
	if err := c2.Call("records.list", nil, &list); err != nil || len(list.Records) != 1 {
		t.Fatalf("restored records: %d %v", len(list.Records), err)
	}
	// And it is anchored going forward.
	if !verifyState(t, d) {
		t.Fatal("restored vault has a root that does not verify")
	}
}
