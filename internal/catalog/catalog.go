// Package catalog maintains the authenticated, monotonic vault-state root.
//
// Every payload in the vault is individually authenticated: the AEAD binds it
// to (vault, record, revision, kind, format, keyVersion), so no ciphertext can
// be forged or moved between records. Nothing, though, authenticates the *set*
// of rows. An attacker with write access to the database file — same UID, so
// within the trust boundary for confidentiality but not for integrity — can:
//
//   - delete a record, and every surviving row still verifies;
//   - flip a client from revoked back to approved, undoing a revocation;
//   - restore an old key_envelopes row, undoing a master-password change so
//     the previous password unlocks again;
//   - roll the whole file back to an earlier copy.
//
// This package closes that gap with an HMAC over the whole catalog, keyed by a
// root-key subkey that only exists while the vault is unlocked, plus a counter
// that only ever increases. The root is recomputed and rewritten inside the
// same transaction as every mutation, and checked at unlock.
//
// # What it does not do
//
// The anchor lives in the database it protects. Replacing the file wholesale
// with an earlier, self-consistent snapshot survives a daemon restart, because
// the only memory of a higher counter died with the process. Detecting that
// needs trusted state outside the file (a TPM NV counter, an O_EXCL sidecar);
// it is a documented follow-up, not something this tier claims. See
// docs/SECURITY.md.
package catalog

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"time"

	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/command"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/query"
)

// domainTag separates this HMAC's input from any other use of the same key,
// and versions the serialization: changing the layout below without changing
// this tag would make old and new roots silently incomparable.
const domainTag = "github.com/m7medVision/albear/v1/catalog-root"

// ErrNoState reports that no vault_state row exists yet — a vault created
// before this table, on its first unlock after migrating. The caller decides
// what to do about it (see the trust-on-first-use bootstrap in vault).
var ErrNoState = errors.New("catalog: no vault state recorded")

// State is one stored anchor.
type State struct {
	Counter int64
	Root    []byte
}

// Root computes the state root over everything visible in tx at this moment.
// counter is folded in, so the same catalog at two different counters produces
// different roots and an old (root, counter) pair cannot be replayed onto a
// newer state.
func Root(ctx context.Context, tx *sql.Tx, key, vaultID []byte, envVersion uint32, counter int64) ([]byte, error) {
	q := query.New(tx)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(domainTag))
	writeBytes(mac, vaultID)
	writeU64(mac, uint64(envVersion))
	writeU64(mac, uint64(counter))

	// Records, ordered by id. Ciphertexts are hashed rather than fed in whole:
	// a digest is fixed-width, which keeps the framing unambiguous, and the
	// ciphertext is already authenticated on its own terms — what matters here
	// is only whether it is the same one.
	records, err := q.ListRecordsForRoot(ctx)
	if err != nil {
		return nil, err
	}
	writeU64(mac, uint64(len(records)))
	for _, r := range records {
		writeBytes(mac, r.RecordID)
		writeU64(mac, uint64(r.Revision))
		writeU64(mac, uint64(r.KeyVersion))
		writeU64(mac, uint64(r.PayloadVersion))
		writeDigest(mac, r.MetadataCiphertext)
		writeDigest(mac, r.SecretCiphertext)
	}

	// Clients, ordered by id. status and capability_mask are the fields a
	// rollback would target; credential_hash and the static key pin identity.
	clients, err := q.ListClientsForRoot(ctx)
	if err != nil {
		return nil, err
	}
	writeU64(mac, uint64(len(clients)))
	for _, c := range clients {
		writeBytes(mac, c.ClientID)
		writeU64(mac, uint64(c.Status))
		writeU64(mac, uint64(c.CapabilityMask))
		writeBytes(mac, c.CredentialHash)
		writeDigest(mac, c.NoiseStaticPubkey)
	}

	// The active envelope: covering it detects an old one being restored to
	// bring a retired master password back to life.
	env, err := q.GetActiveEnvelopeDigest(ctx, int64(envVersion))
	if err != nil {
		return nil, err
	}
	envMac := sha256.New()
	writeBytes(envMac, env.WrappedRootKey)
	writeBytes(envMac, env.KdfSalt)
	writeU64(envMac, uint64(env.KdfMemoryKib))
	writeU64(envMac, uint64(env.KdfIterations))
	writeU64(envMac, uint64(env.KdfParallelism))
	writeBytes(mac, envMac.Sum(nil))

	// security_events is deliberately excluded. It is append-only and its
	// AUTOINCREMENT sequence_id is its own (weaker) anti-deletion signal.
	// Including it would churn the root on every event — including the event
	// recording an integrity failure, which is written while responding to a
	// mismatch.

	return mac.Sum(nil), nil
}

// Stamp advances the counter and rewrites the root, returning the counter it
// wrote. It must be called as the final step inside a mutating transaction, on
// that transaction: stamping afterwards would leave a window where a crash
// yields a stale root and locks the user out of a vault nobody attacked.
//
// The returned counter is what callers feed to the in-process high-water mark
// — but only once the transaction has committed. Noting a counter that never
// landed would make the next unlock see a lower one and cry rollback over
// nothing.
func Stamp(ctx context.Context, tx *sql.Tx, key, vaultID []byte, envVersion uint32, now time.Time) (int64, error) {
	prev, err := Load(ctx, tx)
	next := int64(1)
	switch {
	case err == nil:
		next = prev.Counter + 1
	case errors.Is(err, ErrNoState):
		// First stamp on this vault: start at 1.
	default:
		return 0, err
	}

	root, err := Root(ctx, tx, key, vaultID, envVersion, next)
	if err != nil {
		return 0, err
	}
	if err := command.New(tx).UpsertVaultState(ctx, command.UpsertVaultStateParams{
		StateCounter: next,
		StateRoot:    root,
		UpdatedAtMs:  now.UnixMilli(),
	}); err != nil {
		return 0, err
	}
	return next, nil
}

// Load reads the stored anchor, reporting ErrNoState when there is none.
func Load(ctx context.Context, tx *sql.Tx) (State, error) {
	row, err := query.New(tx).GetVaultState(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, ErrNoState
	}
	if err != nil {
		return State{}, err
	}
	return State{Counter: row.StateCounter, Root: row.StateRoot}, nil
}

// Verify recomputes the root at the stored counter and compares. A mismatch
// means the catalog changed without going through a stamping transaction.
func Verify(ctx context.Context, tx *sql.Tx, key, vaultID []byte, envVersion uint32) (State, bool, error) {
	st, err := Load(ctx, tx)
	if err != nil {
		return State{}, false, err
	}
	want, err := Root(ctx, tx, key, vaultID, envVersion, st.Counter)
	if err != nil {
		return State{}, false, err
	}
	return st, hmac.Equal(want, st.Root), nil
}

// The serialization is length-prefixed throughout. Without it, adjacent
// fields could be re-partitioned — two records could be arranged to hash the
// same as one — which is exactly the forgery this is meant to prevent.

type writer interface{ Write([]byte) (int, error) }

func writeU64(w writer, v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	w.Write(b[:])
}

func writeBytes(w writer, b []byte) {
	writeU64(w, uint64(len(b)))
	w.Write(b)
}

func writeDigest(w writer, b []byte) {
	sum := sha256.Sum256(b)
	w.Write(sum[:])
}
