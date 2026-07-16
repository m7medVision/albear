//go:build linux

package daemon

import (
	"context"
	"encoding/hex"
	"encoding/json"

	accessdomain "github.com/m7medVision/albear/internal/access/domain"
	"github.com/m7medVision/albear/internal/adapters/protocol"
	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	recordsapp "github.com/m7medVision/albear/internal/records/application"
	recdomain "github.com/m7medVision/albear/internal/records/domain"
	secdomain "github.com/m7medVision/albear/internal/security/domain"
	shared "github.com/m7medVision/albear/internal/shared/domain"
)

// connState is the per-connection authorization context.
type connState struct {
	session   *accessdomain.Session
	clientID  shared.ID
	pairing   bool // unpaired Noise_XX channel: pairing operations only
	staticKey []byte
}

// requiredCapability maps operations to capabilities. Operations absent here
// are pairing-channel operations or internal.
var requiredCapability = map[string]accessdomain.Capability{
	"vault.status":            accessdomain.CapVaultStatus,
	"vault.init":              accessdomain.CapClientAdmin,
	"vault.unlock":            accessdomain.CapVaultUnlock,
	"vault.lock":              accessdomain.CapVaultLock,
	"vault.panic":             accessdomain.CapVaultLock,
	"vault.changePassword":    accessdomain.CapPasswordChange,
	"vault.destroy":           accessdomain.CapVaultDestroy,
	"records.create":          accessdomain.CapRecordWrite,
	"records.update":          accessdomain.CapRecordWrite,
	"records.delete":          accessdomain.CapRecordDelete,
	"records.list":            accessdomain.CapRecordList,
	"records.search":          accessdomain.CapRecordList,
	"records.show":            accessdomain.CapRecordRead,
	"records.reveal":          accessdomain.CapRecordReveal,
	"records.match":           accessdomain.CapRecordMatch,
	"records.revealForOrigin": accessdomain.CapRecordRevealForOrigin,
	"records.createLogin":     accessdomain.CapRecordCreateLogin,
	"records.updateLogin":     accessdomain.CapRecordUpdateLogin,
	"password.generate":       accessdomain.CapPasswordGenerate,
	"clients.pending":         accessdomain.CapClientAdmin,
	"clients.approve":         accessdomain.CapClientAdmin,
	"clients.revoke":          accessdomain.CapClientAdmin,
	"clients.list":            accessdomain.CapClientAdmin,
	"backup.create":           accessdomain.CapBackupCreate,
	"backup.verify":           accessdomain.CapBackupCreate,
	"backup.restore":          accessdomain.CapBackupRestore,
	"events.recent":           accessdomain.CapClientAdmin,
}

// pairingOps are the only operations allowed on an unpaired channel.
var pairingOps = map[string]bool{
	"clients.pair":   true,
	"clients.claim":  true,
	"clients.cancel": true,
	"vault.status":   true,
}

// Handle dispatches one decrypted request envelope and returns the response
// envelope. It never returns internal error text to the client.
func (s *Server) Handle(ctx context.Context, st *connState, raw []byte) *protocol.Response {
	var req protocol.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return protocol.ErrResponse("", shared.ErrValidation)
	}
	if req.ProtocolVersion != protocol.Version {
		return protocol.ErrResponse(req.RequestID, shared.ErrValidation)
	}

	if st.pairing {
		if !pairingOps[req.Operation] {
			s.recorder.Record(ctx, secdomain.SeverityWarning, secdomain.EventUnauthorizedRequest, "")
			return protocol.ErrResponse(req.RequestID, shared.ErrAuthorizationDeny)
		}
	} else {
		cap, ok := requiredCapability[req.Operation]
		if !ok {
			return protocol.ErrResponse(req.RequestID, shared.ErrValidation)
		}
		if err := s.authorize(st, cap); err != nil {
			s.recorder.Record(ctx, secdomain.SeverityWarning, secdomain.EventUnauthorizedRequest, "")
			return protocol.ErrResponse(req.RequestID, err)
		}
	}

	data, err := s.dispatch(ctx, st, req.Operation, req.Payload)
	if err != nil {
		return protocol.ErrResponse(req.RequestID, err)
	}
	resp, err := protocol.OKResponse(req.RequestID, data)
	if err != nil {
		return protocol.ErrResponse(req.RequestID, err)
	}
	return resp
}

// authorize validates the connection's session against the current epoch,
// reissuing over the already-authenticated channel when the epoch moved
// (the cryptographic identity did not change; revoked clients are dropped).
func (s *Server) authorize(st *connState, cap accessdomain.Capability) error {
	epoch := s.vault.Epoch()
	if st.session == nil {
		return shared.ErrAuthorizationDeny
	}
	if _, err := s.sessions.Validate(st.session.ID, epoch); err != nil {
		if !st.clientID.IsZero() {
			client, lookupErr := s.access.Lookup(context.Background(), st.clientID)
			if lookupErr != nil || !client.IsApproved() {
				return shared.ErrAuthorizationDeny
			}
		}
		fresh, issueErr := s.sessions.Issue(st.clientID, st.session.Capabilities, epoch)
		if issueErr != nil {
			return shared.ErrAuthorizationDeny
		}
		st.session = fresh
	}
	return st.session.Authorize(cap)
}

func decode[T any](payload json.RawMessage) (T, error) {
	var v T
	if len(payload) == 0 {
		return v, shared.ErrValidation
	}
	if err := json.Unmarshal(payload, &v); err != nil {
		return v, shared.ErrValidation
	}
	return v, nil
}

func (s *Server) dispatch(ctx context.Context, st *connState, op string, payload json.RawMessage) (any, error) {
	switch op {
	case "vault.status":
		return s.opStatus(ctx)
	case "vault.init":
		return s.opInit(ctx, payload)
	case "vault.unlock":
		return s.opUnlock(ctx, st, payload)
	case "vault.lock", "vault.panic":
		s.vault.Lock()
		code := secdomain.EventVaultLocked
		if op == "vault.panic" {
			code = secdomain.EventVaultPanicLocked
		}
		s.recorder.Record(ctx, secdomain.SeverityInfo, code, "")
		return struct{}{}, nil
	case "vault.changePassword":
		return s.opChangePassword(ctx, payload)
	case "vault.destroy":
		return s.opDestroy(ctx, payload)
	case "records.create", "records.createLogin":
		return s.opCreate(ctx, payload)
	case "records.update", "records.updateLogin":
		return s.opUpdate(ctx, payload)
	case "records.delete":
		return s.opDelete(ctx, payload)
	case "records.list":
		entries, err := s.records.List()
		if err != nil {
			return nil, err
		}
		return map[string]any{"records": toRecordViews(entries)}, nil
	case "records.search":
		p, err := decode[queryPayload](payload)
		if err != nil {
			return nil, err
		}
		entries, err := s.records.Search(p.Query)
		if err != nil {
			return nil, err
		}
		return map[string]any{"records": toRecordViews(entries)}, nil
	case "records.match":
		p, err := decode[originPayload](payload)
		if err != nil {
			return nil, err
		}
		entries, err := s.records.Match(p.Origin)
		if err != nil {
			return nil, err
		}
		return map[string]any{"records": toRecordViews(entries)}, nil
	case "records.show":
		p, err := decode[refPayload](payload)
		if err != nil {
			return nil, err
		}
		entry, err := s.records.Resolve(p.Ref)
		if err != nil {
			return nil, err
		}
		return toRecordView(entry), nil
	case "records.reveal":
		return s.opReveal(ctx, payload)
	case "records.revealForOrigin":
		return s.opRevealForOrigin(ctx, payload)
	case "password.generate":
		return s.opGenerate(payload)
	case "clients.pair":
		return s.opPair(ctx, st, payload)
	case "clients.claim":
		return s.opClaim(payload)
	case "clients.cancel":
		return s.opCancel(payload)
	case "clients.pending":
		return s.opPending()
	case "clients.approve":
		return s.opApprove(ctx, payload)
	case "clients.revoke":
		return s.opRevoke(ctx, payload)
	case "clients.list":
		return s.opClients(ctx)
	case "backup.create":
		return s.opBackupCreate(ctx, payload)
	case "backup.verify":
		return s.opBackupVerify(payload)
	case "backup.restore":
		return s.opBackupRestore(ctx, payload)
	case "events.recent":
		return s.opEvents(ctx, payload)
	}
	return nil, shared.ErrValidation
}

func (s *Server) opStatus(ctx context.Context) (any, error) {
	st, err := s.vault.Status(ctx)
	if err != nil {
		return nil, err
	}
	return statusData{
		Initialized: st.Initialized, Unlocked: st.Unlocked,
		Epoch: st.Epoch, RecordCount: st.RecordCount,
	}, nil
}

func (s *Server) opInit(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[passwordPayload](payload)
	if err != nil {
		return nil, err
	}
	if len(p.Password) < 8 {
		return nil, shared.ErrValidation
	}
	if err := s.vault.Init(ctx, []byte(p.Password), s.kdfParams); err != nil {
		return nil, err
	}
	s.recorder.Record(ctx, secdomain.SeverityInfo, secdomain.EventVaultCreated, "")
	return struct{}{}, nil
}

func (s *Server) opUnlock(ctx context.Context, st *connState, payload json.RawMessage) (any, error) {
	p, err := decode[passwordPayload](payload)
	if err != nil {
		return nil, err
	}
	if err := s.vault.Unlock(ctx, []byte(p.Password)); err != nil {
		s.recorder.Record(ctx, secdomain.SeverityWarning, secdomain.EventUnlockFailed, "")
		return nil, err
	}
	if err := s.records.LoadIndex(ctx); err != nil {
		// Index build failure is an integrity event: fail closed.
		s.vault.PanicLock()
		s.recorder.Record(ctx, secdomain.SeverityCritical, secdomain.EventIntegrityFailure, "")
		return nil, err
	}
	// Reissue this connection's session at the new epoch (PRD 15.2 step 12).
	if st.session != nil {
		fresh, err := s.sessions.Issue(st.clientID, st.session.Capabilities, s.vault.Epoch())
		if err == nil {
			st.session = fresh
		}
	}
	s.recorder.Record(ctx, secdomain.SeverityInfo, secdomain.EventVaultUnlocked, "")
	return struct{}{}, nil
}

func (s *Server) opChangePassword(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[changePasswordPayload](payload)
	if err != nil {
		return nil, err
	}
	if len(p.Next) < 8 {
		return nil, shared.ErrValidation
	}
	if err := s.vault.ChangeMasterPassword(ctx, []byte(p.Current), []byte(p.Next), s.kdfParams); err != nil {
		return nil, err
	}
	s.recorder.Record(ctx, secdomain.SeverityInfo, secdomain.EventMasterPasswordChanged, "")
	return struct{}{}, nil
}

func (s *Server) opDestroy(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[passwordPayload](payload)
	if err != nil {
		return nil, err
	}
	if err := s.vault.VerifyPassword(ctx, []byte(p.Password)); err != nil {
		return nil, err
	}
	s.recorder.Record(ctx, secdomain.SeverityCritical, secdomain.EventVaultDestroyed, "")
	if err := s.destroyVault(); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (s *Server) opCreate(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[recordFields](payload)
	if err != nil {
		return nil, err
	}
	t := recdomain.RecordType(p.Type)
	if p.Type == "" {
		t = recdomain.TypeLogin
	}
	meta, secret, err := fieldsToDomain(p)
	if err != nil {
		return nil, err
	}
	id, err := s.records.Create(ctx, t, meta, secret)
	if err != nil {
		return nil, err
	}
	return map[string]string{"id": id.String()}, nil
}

func (s *Server) opUpdate(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[updatePayload](payload)
	if err != nil {
		return nil, err
	}
	id, err := shared.IDFromString(p.ID)
	if err != nil {
		return nil, shared.ErrValidation
	}
	meta, secret, err := fieldsToDomain(p.recordFields)
	if err != nil {
		return nil, err
	}
	if err := s.records.Update(ctx, id, p.ExpectedRevision, meta, secret); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (s *Server) opDelete(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[idPayload](payload)
	if err != nil {
		return nil, err
	}
	id, err := shared.IDFromString(p.ID)
	if err != nil {
		return nil, shared.ErrValidation
	}
	if err := s.records.Delete(ctx, id); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (s *Server) opReveal(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[refPayload](payload)
	if err != nil {
		return nil, err
	}
	entry, err := s.records.Resolve(p.Ref)
	if err != nil {
		return nil, err
	}
	secret, err := s.records.Reveal(ctx, entry.ID)
	if err != nil {
		return nil, err
	}
	defer secret.Wipe()
	return toSecretView(secret), nil
}

func (s *Server) opRevealForOrigin(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[revealForOriginPayload](payload)
	if err != nil {
		return nil, err
	}
	id, err := shared.IDFromString(p.ID)
	if err != nil {
		return nil, shared.ErrValidation
	}
	secret, err := s.records.RevealForOrigin(ctx, id, p.Origin, false)
	if err != nil {
		return nil, err
	}
	defer secret.Wipe()
	entry, _ := s.records.Show(id)
	username := ""
	if entry != nil {
		username = entry.Metadata.Username
	}
	return map[string]string{
		"username": username,
		"password": string(secret.Password.Expose()),
	}, nil
}

func (s *Server) opGenerate(payload json.RawMessage) (any, error) {
	opts := recordsapp.DefaultGenerateOptions
	if len(payload) > 0 {
		p, err := decode[generatePayload](payload)
		if err != nil {
			return nil, err
		}
		if !p.Default {
			if p.Length > 0 {
				opts.Length = p.Length
			}
			if p.Upper || p.Lower || p.Digits || p.Symbols {
				opts.Upper, opts.Lower, opts.Digits, opts.Symbols = p.Upper, p.Lower, p.Digits, p.Symbols
			}
		}
	}
	pw, err := recordsapp.GeneratePassword(opts)
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(pw)
	return map[string]string{"password": string(pw)}, nil
}

func (s *Server) opPair(ctx context.Context, st *connState, payload json.RawMessage) (any, error) {
	p, err := decode[pairPayload](payload)
	if err != nil {
		return nil, err
	}
	staticKey, err := hex.DecodeString(p.StaticKey)
	if err != nil {
		return nil, shared.ErrValidation
	}
	// The static key submitted must be the one that ran this handshake:
	// otherwise a relay could substitute its own.
	if !bytesEqual(staticKey, st.staticKey) {
		return nil, shared.ErrValidation
	}
	pending, err := s.access.RequestPairing(accessdomain.ClientKind(p.Kind), p.Label, staticKey)
	if err != nil {
		return nil, err
	}
	s.recorder.Record(ctx, secdomain.SeverityInfo, secdomain.EventClientPairingRequested, "")
	return map[string]string{
		"pairingId": pending.ID.String(),
		"phrase":    pending.Phrase,
	}, nil
}

func (s *Server) opClaim(payload json.RawMessage) (any, error) {
	p, err := decode[pairingIDPayload](payload)
	if err != nil {
		return nil, err
	}
	id, err := shared.IDFromString(p.PairingID)
	if err != nil {
		return nil, shared.ErrValidation
	}
	clientID, credential, err := s.access.ClaimCredential(id)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"clientId":        clientID.String(),
		"credential":      hex.EncodeToString(credential),
		"daemonStaticKey": hex.EncodeToString(s.staticKey.Public),
	}, nil
}

func (s *Server) opCancel(payload json.RawMessage) (any, error) {
	p, err := decode[pairingIDPayload](payload)
	if err != nil {
		return nil, err
	}
	id, err := shared.IDFromString(p.PairingID)
	if err != nil {
		return nil, shared.ErrValidation
	}
	if err := s.access.Cancel(id); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (s *Server) opPending() (any, error) {
	pending := s.access.ListPending()
	type view struct {
		PairingID string `json:"pairingId"`
		Kind      int    `json:"kind"`
		Label     string `json:"label"`
		Phrase    string `json:"phrase"`
	}
	out := make([]view, 0, len(pending))
	for _, p := range pending {
		out = append(out, view{p.ID.String(), int(p.Kind), p.Label, p.Phrase})
	}
	return map[string]any{"pending": out}, nil
}

func (s *Server) opApprove(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[pairingIDPayload](payload)
	if err != nil {
		return nil, err
	}
	id, err := shared.IDFromString(p.PairingID)
	if err != nil {
		return nil, shared.ErrValidation
	}
	if err := s.access.Approve(ctx, id); err != nil {
		return nil, err
	}
	s.recorder.Record(ctx, secdomain.SeverityInfo, secdomain.EventClientApproved, "")
	return struct{}{}, nil
}

func (s *Server) opRevoke(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[idPayload](payload)
	if err != nil {
		return nil, err
	}
	id, err := shared.IDFromString(p.ID)
	if err != nil {
		return nil, shared.ErrValidation
	}
	if err := s.access.Revoke(ctx, id); err != nil {
		return nil, err
	}
	s.sessions.DropClient(id)
	s.recorder.Record(ctx, secdomain.SeverityInfo, secdomain.EventClientRevoked, "")
	return struct{}{}, nil
}

func (s *Server) opClients(ctx context.Context) (any, error) {
	clients, err := s.access.List(ctx)
	if err != nil {
		return nil, err
	}
	type view struct {
		ID       string `json:"id"`
		Kind     int    `json:"kind"`
		Status   int    `json:"status"`
		Label    string `json:"label"`
		LastSeen int64  `json:"lastSeenMs,omitempty"`
	}
	out := make([]view, 0, len(clients))
	for _, c := range clients {
		v := view{c.ID.String(), int(c.Kind), int(c.Status), c.Label, 0}
		if !c.LastSeen.IsZero() {
			v.LastSeen = c.LastSeen.UnixMilli()
		}
		out = append(out, v)
	}
	return map[string]any{"clients": out}, nil
}

func (s *Server) opBackupCreate(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[pathPayload](payload)
	if err != nil {
		return nil, err
	}
	if err := s.backup.Create(ctx, p.Path); err != nil {
		return nil, err
	}
	s.recorder.Record(ctx, secdomain.SeverityInfo, secdomain.EventBackupCreated, "")
	return map[string]string{"path": p.Path}, nil
}

func (s *Server) opBackupVerify(payload json.RawMessage) (any, error) {
	p, err := decode[pathPayload](payload)
	if err != nil {
		return nil, err
	}
	info, err := s.backup.Verify(p.Path)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"vaultId":     info.VaultID.String(),
		"createdAtMs": info.CreatedAtMs,
		"snapshotLen": info.SnapshotLen,
	}, nil
}

func (s *Server) opBackupRestore(ctx context.Context, payload json.RawMessage) (any, error) {
	p, err := decode[pathPayload](payload)
	if err != nil {
		return nil, err
	}
	if _, err := s.backup.Verify(p.Path); err != nil {
		return nil, err
	}
	if err := s.restoreVault(p.Path); err != nil {
		return nil, err
	}
	s.recorder.Record(ctx, secdomain.SeverityInfo, secdomain.EventBackupRestored, "")
	return struct{}{}, nil
}

func (s *Server) opEvents(ctx context.Context, payload json.RawMessage) (any, error) {
	limit := int64(50)
	if len(payload) > 0 {
		if p, err := decode[limitPayload](payload); err == nil && p.Limit > 0 {
			limit = p.Limit
		}
	}
	events, err := s.recorder.Recent(ctx, limit)
	if err != nil {
		return nil, err
	}
	type view struct {
		Sequence   int64  `json:"sequence"`
		OccurredMs int64  `json:"occurredMs"`
		Severity   int    `json:"severity"`
		Code       int    `json:"code"`
		Details    string `json:"details,omitempty"`
	}
	out := make([]view, 0, len(events))
	for _, e := range events {
		out = append(out, view{e.Sequence, e.OccurredAt.UnixMilli(), int(e.Severity), int(e.Code), e.Details})
	}
	return map[string]any{"events": out}, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var d byte
	for i := range a {
		d |= a[i] ^ b[i]
	}
	return d == 0
}
