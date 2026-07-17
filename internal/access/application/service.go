// Package application implements the Client Access context: pairing,
// approval, revocation, and in-memory session management. Client credentials
// double as Noise PSKs; the database stores only their verifier hash and the
// pinned static key.
package application

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	domain "github.com/m7medVision/albear/internal/access/domain"
	"github.com/m7medVision/albear/internal/catalog"
	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/command"
	shared "github.com/m7medVision/albear/internal/shared/domain"
	vaultapp "github.com/m7medVision/albear/internal/vault/application"
)

// PendingPairing is an in-memory pairing request from an unpaired channel.
type PendingPairing struct {
	ID        shared.ID
	Kind      domain.ClientKind
	Label     string
	StaticKey []byte
	Phrase    string
	CreatedAt time.Time

	approved   bool
	clientID   shared.ID
	credential []byte
}

// Service is the client access application service.
type Service struct {
	mu      sync.Mutex
	store   *sqlite.Store
	keys    *vaultapp.Service
	clock   shared.Clock
	pending map[shared.ID]*PendingPairing

	daemonStaticKey []byte // public; mixed into pairing phrases
}

func NewService(store *sqlite.Store, keys *vaultapp.Service, clock shared.Clock) *Service {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &Service{store: store, keys: keys, clock: clock, pending: map[shared.ID]*PendingPairing{}}
}

// SetDaemonStaticKey provides the daemon's Noise static public key so pairing
// phrases commit to both endpoints (PRD 13.5 step 4).
func (s *Service) SetDaemonStaticKey(pub []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.daemonStaticKey = append([]byte(nil), pub...)
}

// RequestPairing registers a pairing request arriving over an unpaired
// Noise_XX channel and returns the request with its confirmation phrase.
func (s *Service) RequestPairing(kind domain.ClientKind, label string, staticKey []byte) (*PendingPairing, error) {
	if len(staticKey) != 32 {
		return nil, shared.ErrValidation
	}
	// Only browser kinds may pair; administrative kinds would escalate the
	// pairing channel into the full CLI capability set.
	if !kind.IsPairable() || domain.DefaultCapabilities(kind) == 0 {
		return nil, shared.ErrValidation
	}
	idBytes, err := crypto.NewID()
	if err != nil {
		return nil, err
	}
	id, _ := shared.IDFromBytes(idBytes)

	s.mu.Lock()
	defer s.mu.Unlock()
	p := &PendingPairing{
		ID: id, Kind: kind, Label: label,
		StaticKey: append([]byte(nil), staticKey...),
		Phrase:    pairingPhrase(s.daemonStaticKey, staticKey, idBytes),
		CreatedAt: s.clock.Now(),
	}
	s.pending[id] = p
	return p, nil
}

// pairingPhrase commits to both static public keys and the request ID. A
// man-in-the-middle bridge that substitutes keys produces a different phrase
// on the two sides, which the user's comparison catches.
func pairingPhrase(daemonPub, clientPub, pairingID []byte) string {
	h := sha256.New()
	h.Write([]byte("albear-pairing-v1"))
	h.Write(daemonPub)
	h.Write(clientPub)
	h.Write(pairingID)
	sum := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("%s-%s-%s", sum[0:4], sum[4:8], sum[8:12])
}

// ListPending returns pairing requests awaiting approval.
func (s *Service) ListPending() []*PendingPairing {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*PendingPairing, 0, len(s.pending))
	for _, p := range s.pending {
		if !p.approved {
			out = append(out, p)
		}
	}
	return out
}

// Approve registers the pending client, pins its static key, and issues its
// credential. Requires an unlocked vault: the label is stored encrypted under
// the audit key. The raw credential is handed back exactly once, over the
// pairing channel, and only its verifier is persisted.
func (s *Service) Approve(ctx context.Context, pairingID shared.ID) error {
	kr, err := s.keys.Keys()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[pairingID]
	if !ok || p.approved {
		return shared.ErrClientNotFound
	}

	clientIDBytes, err := crypto.NewID()
	if err != nil {
		return err
	}
	credential, err := crypto.NewCredential()
	if err != nil {
		return err
	}

	vaultID, _, _, err := s.keys.VaultInfo()
	if err != nil {
		return err
	}
	labelNonce, err := crypto.NewNonce()
	if err != nil {
		return err
	}
	labelAAD := crypto.RecordAAD(vaultID, clientIDBytes, 0, crypto.PayloadLabel, 1, 0)
	labelCT, err := crypto.Seal(kr.Audit, labelNonce, []byte(p.Label), labelAAD)
	if err != nil {
		return err
	}

	var stamped int64
	err = s.store.CommandTx(ctx, func(tx *sql.Tx, c *command.Queries) error {
		if err := c.InsertClient(ctx, command.InsertClientParams{
			ClientID:          clientIDBytes,
			ClientKind:        int64(p.Kind),
			Status:            int64(domain.StatusApproved),
			CapabilityMask:    int64(domain.DefaultCapabilities(p.Kind)),
			CredentialHash:    crypto.CredentialVerifier(credential),
			NoiseStaticPubkey: p.StaticKey,
			LabelNonce:        labelNonce,
			LabelCiphertext:   labelCT,
			CreatedAtMs:       s.clock.Now().UnixMilli(),
		}); err != nil {
			return err
		}
		var err error
		stamped, err = s.stamp(ctx, tx, kr)
		return err
	})
	if err != nil {
		return err
	}
	s.keys.NoteCatalogCounter(stamped)

	p.approved = true
	p.clientID, _ = shared.IDFromBytes(clientIDBytes)
	p.credential = credential
	return nil
}

// Cancel drops a not-yet-approved pairing request. Approved pairings must
// be revoked via the client row, not cancelled, so the credential remains
// retrievable for the pairing channel that already completed the handshake.
func (s *Service) Cancel(pairingID shared.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[pairingID]
	if !ok {
		return shared.ErrClientNotFound
	}
	if p.approved {
		return shared.ErrConflict
	}
	delete(s.pending, pairingID)
	return nil
}

// ClaimCredential is polled by the waiting pairing client. It returns the
// issued identity exactly once and then forgets the raw credential.
func (s *Service) ClaimCredential(pairingID shared.ID) (clientID shared.ID, credential []byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[pairingID]
	if !ok {
		return shared.ID{}, nil, shared.ErrClientNotFound
	}
	if !p.approved {
		return shared.ID{}, nil, shared.ErrAuthorizationDeny
	}
	cred := p.credential
	id := p.clientID
	delete(s.pending, pairingID)
	if cred == nil {
		return shared.ID{}, nil, shared.ErrClientNotFound
	}
	return id, cred, nil
}

// ClientInfo is a redacted view for listings.
type ClientInfo struct {
	ID       shared.ID
	Kind     domain.ClientKind
	Status   domain.ClientStatus
	Label    string
	LastSeen time.Time
}

// List returns registered clients; labels decrypt only while unlocked.
func (s *Service) List(ctx context.Context) ([]ClientInfo, error) {
	rows, err := s.store.Query().ListClients(ctx)
	if err != nil {
		return nil, err
	}
	kr, keysErr := s.keys.Keys()
	var vaultID []byte
	if keysErr == nil {
		vaultID, _, _, _ = s.keys.VaultInfo()
	}
	out := make([]ClientInfo, 0, len(rows))
	for _, r := range rows {
		id, err := shared.IDFromBytes(r.ClientID)
		if err != nil {
			return nil, shared.ErrIntegrityFailure
		}
		info := ClientInfo{
			ID: id, Kind: domain.ClientKind(r.ClientKind), Status: domain.ClientStatus(r.Status),
			Label: "(locked)",
		}
		if keysErr == nil {
			aad := crypto.RecordAAD(vaultID, r.ClientID, 0, crypto.PayloadLabel, 1, 0)
			if label, err := crypto.Open(kr.Audit, r.LabelNonce, r.LabelCiphertext, aad); err == nil {
				info.Label = string(label)
			} else {
				info.Label = "(integrity failure)"
			}
		}
		if r.LastSeenAtMs.Valid {
			info.LastSeen = time.UnixMilli(r.LastSeenAtMs.Int64)
		}
		out = append(out, info)
	}
	return out, nil
}

// Revoke marks a client revoked: its PSK stops completing handshakes.
func (s *Service) Revoke(ctx context.Context, clientID shared.ID) error {
	// Revocation must be anchored: flipping status back to approved in the
	// database file would otherwise silently reinstate a client the user threw
	// out, and every row would still verify.
	kr, err := s.keys.Keys()
	if err != nil {
		return err
	}
	var rows, stamped int64
	err = s.store.CommandTx(ctx, func(tx *sql.Tx, c *command.Queries) error {
		var err error
		rows, err = c.UpdateClientStatus(ctx, command.UpdateClientStatusParams{
			Status: int64(domain.StatusRevoked), ClientID: clientID.Bytes(),
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		stamped, err = s.stamp(ctx, tx, kr)
		return err
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return shared.ErrClientNotFound
	}
	s.keys.NoteCatalogCounter(stamped)
	return nil
}

// stamp re-anchors the vault state inside the caller's transaction.
// It returns the counter written; the caller reports it to the vault's
// high-water mark only after the transaction commits.
func (s *Service) stamp(ctx context.Context, tx *sql.Tx, kr *vaultapp.Keyring) (int64, error) {
	vaultID, _, keyVersion, err := s.keys.VaultInfo()
	if err != nil {
		return 0, err
	}
	return catalog.Stamp(ctx, tx, kr.Catalog, vaultID, keyVersion, s.clock.Now())
}

// Lookup fetches a client for handshake authorization: returns its PSK (the
// credential verifier), pinned static key, status, and capabilities.
func (s *Service) Lookup(ctx context.Context, clientID shared.ID) (*domain.Client, error) {
	row, err := s.store.Query().GetClient(ctx, clientID.Bytes())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, shared.ErrClientNotFound
	}
	if err != nil {
		return nil, err
	}
	return &domain.Client{
		ID:             clientID,
		Kind:           domain.ClientKind(row.ClientKind),
		Status:         domain.ClientStatus(row.Status),
		Capabilities:   domain.CapabilitySet(row.CapabilityMask),
		CredentialHash: row.CredentialHash,
		StaticKey:      row.NoiseStaticPubkey,
	}, nil
}

// TouchLastSeen records client activity.
func (s *Service) TouchLastSeen(ctx context.Context, clientID shared.ID) error {
	return s.store.Command(ctx, func(c *command.Queries) error {
		return c.UpdateClientLastSeen(ctx, command.UpdateClientLastSeenParams{
			LastSeenAtMs: sql.NullInt64{Int64: s.clock.Now().UnixMilli(), Valid: true},
			ClientID:     clientID.Bytes(),
		})
	})
}
