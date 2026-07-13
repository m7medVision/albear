// Package daemon wires every bounded context into the vaultd process: socket
// listener, Noise handshakes, session issuance, request routing, and the
// restore/destroy lifecycle operations.
package daemon

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"sync"

	flynnnoise "github.com/flynn/noise"

	accessapp "albear/internal/access/application"
	accessdomain "albear/internal/access/domain"
	backupapp "albear/internal/backup/application"
	"albear/internal/infrastructure/crypto"
	"albear/internal/infrastructure/ipc"
	"albear/internal/infrastructure/sqlite"
	transport "albear/internal/infrastructure/transport/noise"
	recordsapp "albear/internal/records/application"
	secapp "albear/internal/security/application"
	secdomain "albear/internal/security/domain"
	shared "albear/internal/shared/domain"
	vaultapp "albear/internal/vault/application"
)

// ModeCLI (transport.ModeCLI) is honored only on peer-credential-verified
// direct socket connections; vault-native refuses to relay it (PRD 12.3).
const ModeCLI = transport.ModeCLI

type Server struct {
	log       *slog.Logger
	store     *sqlite.Store
	dbPath    string
	staticKey flynnnoise.DHKey
	kdfParams crypto.KDFParams

	vault    *vaultapp.Service
	records  *recordsapp.Service
	access   *accessapp.Service
	sessions *accessapp.SessionManager
	recorder *secapp.Recorder
	backup   *backupapp.Service

	mu       sync.Mutex
	shutdown func()
}

// New assembles the full service graph over an opened, migrated store.
func New(log *slog.Logger, store *sqlite.Store, dbPath string, staticKey flynnnoise.DHKey, kdf crypto.KDFParams) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	vault := vaultapp.NewService(store, nil)
	records := recordsapp.NewService(store, vault, nil)
	access := accessapp.NewService(store, vault, nil)
	access.SetDaemonStaticKey(staticKey.Public)
	sessions := accessapp.NewSessionManager(nil)
	recorder := secapp.NewRecorder(store, vault, nil)
	backup := backupapp.NewService(store, vault, nil)

	// Lock hooks: destroy the index and every session (PRD 15.3).
	vault.OnLock(records.ClearIndex)
	vault.OnLock(sessions.InvalidateAll)

	return &Server{
		log: log, store: store, dbPath: dbPath, staticKey: staticKey, kdfParams: kdf,
		vault: vault, records: records, access: access,
		sessions: sessions, recorder: recorder, backup: backup,
	}
}

// Vault exposes the vault service (used by vaultd main for shutdown locking).
func (s *Server) Vault() *vaultapp.Service { return s.vault }

// Serve accepts connections until ctx is done.
func (s *Server) Serve(ctx context.Context, ln *net.UnixListener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()

	// Layer 1: OS peer credentials. Only the daemon's own user may connect.
	if err := ipc.VerifyPeer(conn); err != nil {
		s.log.Warn("peer rejected", "category", "ipc")
		s.recorder.Record(ctx, secdomain.SeverityWarning, secdomain.EventUnauthorizedRequest, "")
		return
	}

	// Layer 2: Noise handshake (PRD 12.4).
	nc, hello, remoteStatic, err := transport.ServerHandshake(conn, s.staticKey, s.lookupClient)
	if err != nil {
		s.log.Warn("handshake failed", "category", "transport")
		s.recorder.Record(ctx, secdomain.SeverityWarning, secdomain.EventTransportHandshakeFailed, "")
		return
	}

	st, err := s.buildConnState(ctx, hello, remoteStatic)
	if err != nil {
		s.recorder.Record(ctx, secdomain.SeverityWarning, secdomain.EventTransportHandshakeFailed, "")
		return
	}

	for {
		raw, err := nc.Recv()
		if err != nil {
			if errors.Is(err, transport.ErrTransportAEAD) {
				// Level 2 response: tampered frame → disconnect (PRD 19.1).
				s.recorder.Record(ctx, secdomain.SeverityWarning, secdomain.EventProtocolViolation, "")
			}
			return
		}
		resp := s.Handle(ctx, st, raw)
		out, err := json.Marshal(resp)
		if err != nil {
			return
		}
		if err := nc.Send(out); err != nil {
			return
		}
	}
}

// lookupClient resolves paired hellos to their PSK and pinned static key.
// The stored credential verifier IS the PSK (see crypto.CredentialVerifier).
func (s *Server) lookupClient(h transport.Hello) ([]byte, []byte, error) {
	if h.Mode != transport.ModePaired {
		return nil, nil, transport.ErrUnknownMode
	}
	id, err := shared.IDFromString(h.ClientID)
	if err != nil {
		return nil, nil, shared.ErrClientNotFound
	}
	client, err := s.access.Lookup(context.Background(), id)
	if err != nil {
		return nil, nil, err
	}
	if !client.IsApproved() {
		return nil, nil, shared.ErrAuthorizationDeny
	}
	return client.CredentialHash, client.StaticKey, nil
}

// buildConnState issues the connection's session according to the hello mode.
func (s *Server) buildConnState(ctx context.Context, hello *transport.Hello, remoteStatic []byte) (*connState, error) {
	epoch := s.vault.Epoch()
	switch hello.Mode {
	case ModeCLI:
		// Same-user direct connection: auto-authorized with CLI capabilities
		// (PRD 12.3). Peer credentials were verified before the handshake.
		session, err := s.sessions.Issue(shared.ID{}, accessdomain.CLICapabilities, epoch)
		if err != nil {
			return nil, err
		}
		return &connState{session: session, staticKey: remoteStatic}, nil
	case transport.ModePairing:
		return &connState{pairing: true, staticKey: remoteStatic}, nil
	case transport.ModePaired:
		id, err := shared.IDFromString(hello.ClientID)
		if err != nil {
			return nil, shared.ErrClientNotFound
		}
		client, err := s.access.Lookup(ctx, id)
		if err != nil {
			return nil, err
		}
		session, err := s.sessions.Issue(id, client.Capabilities, epoch)
		if err != nil {
			return nil, err
		}
		s.access.TouchLastSeen(ctx, id)
		return &connState{session: session, clientID: id, staticKey: remoteStatic}, nil
	}
	return nil, transport.ErrUnknownMode
}

// restoreVault swaps the database for a verified backup snapshot (PRD 22.2).
func (s *Server) restoreVault(backupPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.vault.Lock()
	if err := s.store.Close(); err != nil {
		return err
	}
	err := backupapp.Restore(backupPath, s.dbPath, func(candidate string) error {
		db, err := sqlite.Open(candidate)
		if err != nil {
			return err
		}
		defer db.Close()
		_, err = sqlite.NewStore(db).Query().GetVault(context.Background())
		return err
	})
	if err != nil {
		// Reopen whatever is on disk so the daemon stays alive.
		if db, openErr := sqlite.Open(s.dbPath); openErr == nil {
			s.store.Swap(db)
		}
		return err
	}
	db, err := sqlite.Open(s.dbPath)
	if err != nil {
		return err
	}
	if err := sqlite.Migrate(context.Background(), db); err != nil {
		db.Close()
		return err
	}
	s.store.Swap(db)
	s.vault.Reset()
	return nil
}

// destroyVault permanently removes the vault files (PRD 19.1 Level 4). The
// caller has already verified the master password interactively.
func (s *Server) destroyVault() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vault.Lock()
	if err := s.store.Close(); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(s.dbPath + suffix)
	}
	// Reopen a fresh, empty database so the daemon keeps serving (status now
	// reports uninitialized) instead of crashing on later requests.
	db, err := sqlite.Open(s.dbPath)
	if err != nil {
		return err
	}
	if err := sqlite.Migrate(context.Background(), db); err != nil {
		db.Close()
		return err
	}
	s.store.Swap(db)
	s.vault.Reset()
	if s.shutdown != nil {
		go s.shutdown()
	}
	return nil
}

// OnDestroy registers a callback fired after vault destruction (daemon exit).
func (s *Server) OnDestroy(fn func()) { s.shutdown = fn }

// DaemonStaticPublicKey exposes the transport identity for doctor output.
func (s *Server) DaemonStaticPublicKey() string {
	return hex.EncodeToString(s.staticKey.Public)
}
