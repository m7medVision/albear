// Package application implements the Vault Security context use cases:
// creation, unlock, lock, panic lock, and master-password change. This is the
// only package that ever holds the root vault key or its derived subkeys.
package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/command"
	shared "github.com/m7medVision/albear/internal/shared/domain"
	domain "github.com/m7medVision/albear/internal/vault/domain"

	"database/sql"
)

const canaryPlaintext = "albear-canary-v1"

// Keyring holds the derived subkeys for one unlocked period. It lives only in
// daemon memory and is wiped on lock.
type Keyring struct {
	Metadata []byte
	Secret   []byte
	Audit    []byte
	Backup   []byte
}

func (k *Keyring) wipe() {
	crypto.Zero(k.Metadata)
	crypto.Zero(k.Secret)
	crypto.Zero(k.Audit)
	crypto.Zero(k.Backup)
}

// ErrRateLimited is returned when unlock attempts arrive faster than the
// failure backoff allows (PRD 19.3).
var ErrRateLimited = errors.New("vault: unlock rate limited")

// Service is the vault security application service.
type Service struct {
	mu    sync.Mutex
	store *sqlite.Store
	clock shared.Clock

	vault   domain.Vault
	loaded  bool
	keyring *Keyring
	rootKey []byte

	failedUnlocks int
	nextUnlockAt  time.Time

	onLock []func()
}

func NewService(store *sqlite.Store, clock shared.Clock) *Service {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &Service{store: store, clock: clock}
}

// OnLock registers a callback invoked (under lock) whenever the vault locks:
// session invalidation and index destruction hook in here.
func (s *Service) OnLock(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onLock = append(s.onLock, fn)
}

// Status returns the current state without touching key material.
type Status struct {
	Initialized bool
	Unlocked    bool
	Epoch       uint64
	RecordCount int64
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{}
	if err := s.loadVault(ctx); err != nil {
		if errors.Is(err, shared.ErrVaultNotFound) {
			return st, nil
		}
		return st, err
	}
	st.Initialized = true
	st.Unlocked = s.vault.IsUnlocked()
	st.Epoch = s.vault.Epoch
	if st.Unlocked {
		n, err := s.store.Query().CountRecords(ctx)
		if err != nil {
			return st, err
		}
		st.RecordCount = n
	}
	return st, nil
}

// Epoch returns the current vault epoch for session binding.
func (s *Service) Epoch() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vault.Epoch
}

// IsUnlocked reports the lock state without touching key material or the
// database, so authorization and the idle-lock loop can consult it cheaply.
func (s *Service) IsUnlocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vault.IsUnlocked()
}

// LockPolicy returns the loaded vault's lock policy. It is the zero policy
// until a vault is loaded, so callers must gate on IsUnlocked first.
func (s *Service) LockPolicy() domain.LockPolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vault.LockPolicy
}

// Init creates the vault: random root key, Argon2id envelope, verified canary
// (PRD 15.1). It fails if a vault already exists.
func (s *Service) Init(ctx context.Context, password []byte, params crypto.KDFParams) error {
	defer crypto.Zero(password)
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadVault(ctx); err == nil {
		return shared.ErrVaultExists
	} else if !errors.Is(err, shared.ErrVaultNotFound) {
		return err
	}
	if err := params.Validate(); err != nil {
		return err
	}

	vaultID, err := crypto.NewID()
	if err != nil {
		return err
	}
	rootKey, err := crypto.NewKey()
	if err != nil {
		return err
	}
	defer crypto.Zero(rootKey)

	env, err := buildEnvelope(vaultID, 1, rootKey, password, params)
	if err != nil {
		return err
	}

	now := s.clock.Now().UnixMilli()
	err = s.store.Command(ctx, func(c *command.Queries) error {
		if err := c.InsertVault(ctx, command.InsertVaultParams{
			VaultID: vaultID, FormatVersion: int64(domain.FormatVersion),
			ActiveEnvelopeVersion: 1, CreatedAtMs: now, UpdatedAtMs: now,
		}); err != nil {
			return err
		}
		env.CreatedAtMs = now
		return c.InsertKeyEnvelope(ctx, *env)
	})
	if err != nil {
		return err
	}

	id, _ := shared.IDFromBytes(vaultID)
	s.vault = domain.Vault{
		ID: id, State: domain.StateLocked,
		FormatVersion: domain.FormatVersion, ActiveEnvelopeVersion: 1,
		LockPolicy: domain.DefaultLockPolicy,
	}
	s.loaded = true
	return nil
}

// buildEnvelope wraps rootKey under an Argon2id-derived KEK and produces the
// envelope row including a verification canary.
func buildEnvelope(vaultID []byte, version uint32, rootKey, password []byte, params crypto.KDFParams) (*command.InsertKeyEnvelopeParams, error) {
	salt, err := crypto.NewSalt()
	if err != nil {
		return nil, err
	}
	kek, err := crypto.DeriveKEK(password, salt, params)
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(kek)

	wrapNonce, err := crypto.NewNonce()
	if err != nil {
		return nil, err
	}
	wrapAAD := envelopeAAD(vaultID, version, "wrap")
	wrapped, err := crypto.Seal(kek, wrapNonce, rootKey, wrapAAD)
	if err != nil {
		return nil, err
	}

	canaryNonce, err := crypto.NewNonce()
	if err != nil {
		return nil, err
	}
	canaryAAD := envelopeAAD(vaultID, version, "canary")
	canary, err := crypto.Seal(rootKey, canaryNonce, []byte(canaryPlaintext), canaryAAD)
	if err != nil {
		return nil, err
	}

	return &command.InsertKeyEnvelopeParams{
		EnvelopeVersion: int64(version), VaultID: vaultID,
		KdfAlgorithm: "argon2id", KdfVersion: 19,
		KdfSalt:      salt,
		KdfMemoryKib: int64(params.MemoryKiB), KdfIterations: int64(params.Iterations),
		KdfParallelism: int64(params.Parallelism),
		WrapAlgorithm:  "xchacha20poly1305",
		WrapNonce:      wrapNonce, WrappedRootKey: wrapped,
		CanaryNonce: canaryNonce, EncryptedCanary: canary,
	}, nil
}

func envelopeAAD(vaultID []byte, version uint32, kind string) []byte {
	aad := append([]byte(nil), vaultID...)
	aad = append(aad, byte(version>>24), byte(version>>16), byte(version>>8), byte(version))
	return append(aad, kind...)
}

// Unlock derives the KEK, unwraps the root key, verifies the canary, derives
// subkeys, and bumps the epoch (PRD 15.2). Every failure mode returns the
// same generic authentication error.
func (s *Service) Unlock(ctx context.Context, password []byte) error {
	defer crypto.Zero(password)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	if now.Before(s.nextUnlockAt) {
		return ErrRateLimited
	}
	if err := s.loadVault(ctx); err != nil {
		return err
	}

	env, err := s.store.Query().GetKeyEnvelope(ctx, int64(s.vault.ActiveEnvelopeVersion))
	if err != nil {
		return shared.ErrIntegrityFailure
	}
	kek, err := crypto.DeriveKEK(password, env.KdfSalt, crypto.KDFParams{
		MemoryKiB:   uint32(env.KdfMemoryKib),
		Iterations:  uint32(env.KdfIterations),
		Parallelism: uint8(env.KdfParallelism),
	})
	if err != nil {
		return s.unlockFailed()
	}
	defer crypto.Zero(kek)

	vaultID := s.vault.ID.Bytes()
	version := uint32(env.EnvelopeVersion)
	rootKey, err := crypto.Open(kek, env.WrapNonce, env.WrappedRootKey, envelopeAAD(vaultID, version, "wrap"))
	if err != nil {
		return s.unlockFailed()
	}
	canary, err := crypto.Open(rootKey, env.CanaryNonce, env.EncryptedCanary, envelopeAAD(vaultID, version, "canary"))
	if err != nil || string(canary) != canaryPlaintext {
		crypto.Zero(rootKey)
		return s.unlockFailed()
	}

	kr, err := deriveKeyring(rootKey)
	if err != nil {
		crypto.Zero(rootKey)
		return err
	}
	s.rootKey = rootKey
	s.keyring = kr
	s.failedUnlocks = 0
	s.nextUnlockAt = time.Time{}
	s.vault.Unlock()
	return nil
}

func deriveKeyring(rootKey []byte) (*Keyring, error) {
	kr := &Keyring{}
	for _, d := range []struct {
		label string
		dst   *[]byte
	}{
		{crypto.LabelMetadata, &kr.Metadata},
		{crypto.LabelSecrets, &kr.Secret},
		{crypto.LabelAudit, &kr.Audit},
		{crypto.LabelBackup, &kr.Backup},
	} {
		k, err := crypto.DeriveSubkey(rootKey, d.label)
		if err != nil {
			return nil, err
		}
		*d.dst = k
	}
	return kr, nil
}

// unlockFailed applies the escalating delay schedule from PRD 19.3 and always
// returns the generic authentication error.
func (s *Service) unlockFailed() error {
	s.failedUnlocks++
	var delay time.Duration
	switch {
	case s.failedUnlocks <= 3:
		delay = 0
	case s.failedUnlocks <= 5:
		delay = 2 * time.Second
	case s.failedUnlocks <= 10:
		delay = time.Duration(s.failedUnlocks-5) * 5 * time.Second
	default:
		delay = 60 * time.Second
	}
	s.nextUnlockAt = s.clock.Now().Add(delay)
	return shared.ErrAuthenticationFail
}

// Lock clears key material, bumps the epoch, and fires lock callbacks
// (PRD 15.3). Locking an already locked vault is a no-op.
func (s *Service) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lockLocked()
}

func (s *Service) lockLocked() {
	if s.keyring != nil {
		s.keyring.wipe()
		s.keyring = nil
	}
	if s.rootKey != nil {
		crypto.Zero(s.rootKey)
		s.rootKey = nil
	}
	if s.vault.IsUnlocked() {
		s.vault.Lock()
	}
	for _, fn := range s.onLock {
		fn()
	}
}

// PanicLock is the Level 3 response (PRD 19.1): identical to Lock today, kept
// as a separate entry point so callers express intent and events differ.
func (s *Service) PanicLock() { s.Lock() }

// Reset locks and forgets the cached vault row so the next operation reloads
// it from the (possibly replaced) database. Used after backup restore.
func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lockLocked()
	s.loaded = false
	s.vault = domain.Vault{}
}

// Keys returns the keyring while unlocked. Callers must not retain it across
// operations; it is invalidated on lock.
func (s *Service) Keys() (*Keyring, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil || !s.vault.IsUnlocked() {
		return nil, shared.ErrVaultLocked
	}
	return s.keyring, nil
}

// VaultInfo exposes identity values needed for AADs.
func (s *Service) VaultInfo() (vaultID []byte, formatVersion, keyVersion uint32, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		return nil, 0, 0, shared.ErrVaultNotFound
	}
	return s.vault.ID.Bytes(), s.vault.FormatVersion, s.vault.ActiveEnvelopeVersion, nil
}

// ChangeMasterPassword re-wraps only the root key under a new KEK and swaps
// the envelope atomically, then locks (PRD 15.6).
func (s *Service) ChangeMasterPassword(ctx context.Context, current, next []byte, params crypto.KDFParams) error {
	defer crypto.Zero(current)
	defer crypto.Zero(next)
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadVault(ctx); err != nil {
		return err
	}
	oldVersion := int64(s.vault.ActiveEnvelopeVersion)
	env, err := s.store.Query().GetKeyEnvelope(ctx, oldVersion)
	if err != nil {
		return shared.ErrIntegrityFailure
	}

	kek, err := crypto.DeriveKEK(current, env.KdfSalt, crypto.KDFParams{
		MemoryKiB:   uint32(env.KdfMemoryKib),
		Iterations:  uint32(env.KdfIterations),
		Parallelism: uint8(env.KdfParallelism),
	})
	if err != nil {
		return shared.ErrAuthenticationFail
	}
	defer crypto.Zero(kek)

	vaultID := s.vault.ID.Bytes()
	rootKey, err := crypto.Open(kek, env.WrapNonce, env.WrappedRootKey,
		envelopeAAD(vaultID, uint32(oldVersion), "wrap"))
	if err != nil {
		return shared.ErrAuthenticationFail
	}
	defer crypto.Zero(rootKey)

	newVersion := uint32(oldVersion + 1)
	newEnv, err := buildEnvelope(vaultID, newVersion, rootKey, next, params)
	if err != nil {
		return err
	}
	now := s.clock.Now().UnixMilli()
	newEnv.CreatedAtMs = now

	err = s.store.Command(ctx, func(c *command.Queries) error {
		if err := c.InsertKeyEnvelope(ctx, *newEnv); err != nil {
			return err
		}
		if err := c.SetActiveEnvelope(ctx, command.SetActiveEnvelopeParams{
			ActiveEnvelopeVersion: int64(newVersion), UpdatedAtMs: now,
		}); err != nil {
			return err
		}
		return c.DeleteKeyEnvelope(ctx, oldVersion)
	})
	if err != nil {
		return err
	}

	s.vault.ActiveEnvelopeVersion = newVersion
	s.lockLocked()
	return nil
}

// VerifyPassword checks the master password without changing state, for
// reauthentication before destructive operations.
func (s *Service) VerifyPassword(ctx context.Context, password []byte) error {
	defer crypto.Zero(password)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadVault(ctx); err != nil {
		return err
	}
	env, err := s.store.Query().GetKeyEnvelope(ctx, int64(s.vault.ActiveEnvelopeVersion))
	if err != nil {
		return shared.ErrIntegrityFailure
	}
	kek, err := crypto.DeriveKEK(password, env.KdfSalt, crypto.KDFParams{
		MemoryKiB:   uint32(env.KdfMemoryKib),
		Iterations:  uint32(env.KdfIterations),
		Parallelism: uint8(env.KdfParallelism),
	})
	if err != nil {
		return shared.ErrAuthenticationFail
	}
	defer crypto.Zero(kek)
	rk, err := crypto.Open(kek, env.WrapNonce, env.WrappedRootKey,
		envelopeAAD(s.vault.ID.Bytes(), s.vault.ActiveEnvelopeVersion, "wrap"))
	if err != nil {
		return shared.ErrAuthenticationFail
	}
	crypto.Zero(rk)
	return nil
}

func (s *Service) loadVault(ctx context.Context) error {
	if s.loaded {
		return nil
	}
	row, err := s.store.Query().GetVault(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return shared.ErrVaultNotFound
	}
	if err != nil {
		return err
	}
	if uint32(row.FormatVersion) > domain.FormatVersion {
		return shared.ErrIntegrityFailure
	}
	id, err := shared.IDFromBytes(row.VaultID)
	if err != nil {
		return shared.ErrIntegrityFailure
	}
	s.vault = domain.Vault{
		ID: id, State: domain.StateLocked,
		FormatVersion:         uint32(row.FormatVersion),
		ActiveEnvelopeVersion: uint32(row.ActiveEnvelopeVersion),
		LockPolicy:            domain.DefaultLockPolicy,
	}
	s.loaded = true
	return nil
}
