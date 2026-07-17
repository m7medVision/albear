// Package application implements the Backup and Recovery context: a versioned
// authenticated container around a consistent SQLite snapshot (PRD 22).
package application

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	shared "github.com/m7medVision/albear/internal/shared/domain"
	vaultapp "github.com/m7medVision/albear/internal/vault/application"
)

var magic = []byte("ALBEARBK")

const containerVersion uint32 = 1

var (
	ErrBadContainer = errors.New("backup: invalid or corrupt container")
	ErrWrongVault   = errors.New("backup: container belongs to a different vault")
)

// Service creates, verifies, and restores encrypted backups.
type Service struct {
	store *sqlite.Store
	keys  *vaultapp.Service
	clock shared.Clock
}

func NewService(store *sqlite.Store, keys *vaultapp.Service, clock shared.Clock) *Service {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &Service{store: store, keys: keys, clock: clock}
}

// Container layout (all integers big endian):
//
//	magic[8] version[4] vaultID[16] createdAtMs[8] snapshotLen[8]
//	snapshot[snapshotLen] hmac[32]
//
// The HMAC (backup subkey) covers everything before it. The snapshot itself
// is the encrypted SQLite database: secrets inside stay ciphertext.

// Create writes a backup container to destPath (mode 0600).
func (s *Service) Create(ctx context.Context, destPath string) error {
	kr, err := s.keys.Keys()
	if err != nil {
		return err
	}
	vaultID, _, _, err := s.keys.VaultInfo()
	if err != nil {
		return err
	}

	tmpSnap := destPath + ".snap.tmp"
	defer os.Remove(tmpSnap)
	if err := s.store.Snapshot(ctx, tmpSnap); err != nil {
		return fmt.Errorf("backup: snapshot: %w", err)
	}
	snap, err := os.ReadFile(tmpSnap)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.Write(magic)
	binary.Write(&buf, binary.BigEndian, containerVersion)
	buf.Write(vaultID)
	binary.Write(&buf, binary.BigEndian, uint64(s.clock.Now().UnixMilli()))
	binary.Write(&buf, binary.BigEndian, uint64(len(snap)))
	buf.Write(snap)

	mac := hmac.New(sha256.New, kr.Backup)
	mac.Write(buf.Bytes())
	buf.Write(mac.Sum(nil))

	tmpDest := destPath + ".tmp"
	if err := os.WriteFile(tmpDest, buf.Bytes(), 0o600); err != nil {
		return err
	}
	// Atomic publish: a partially written backup is never valid (PRD 21).
	return os.Rename(tmpDest, destPath)
}

// Info is the parsed, verified container header.
type Info struct {
	VaultID     shared.ID
	CreatedAtMs uint64
	SnapshotLen uint64
}

// Verify authenticates a container against the current vault's backup key
// without touching the live database (PRD 22.2). It returns the snapshot bytes
// the HMAC actually covered.
//
// Returning the buffer is what makes restore safe: the caller installs these
// bytes rather than re-reading the path, so there is no window in which the
// file on disk can be swapped between the check and the install.
func (s *Service) Verify(path string) (*Info, []byte, error) {
	kr, err := s.keys.Keys()
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	c, err := parseContainer(raw)
	if err != nil {
		return nil, nil, err
	}
	mac := hmac.New(sha256.New, kr.Backup)
	mac.Write(c.body)
	if !hmac.Equal(mac.Sum(nil), c.tag) {
		return nil, nil, ErrBadContainer
	}
	vaultID, _, _, err := s.keys.VaultInfo()
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(c.info.VaultID.Bytes(), vaultID) {
		return nil, nil, ErrWrongVault
	}
	return c.info, c.snapshot, nil
}

// Restore atomically installs an authenticated snapshot as the vault database.
// The caller (daemon) must have closed the database first and re-opens after.
// The previous database is kept at dbPath+".recovery" until the restored file
// is verified openable (PRD 22.2).
//
// It takes the snapshot bytes rather than a path deliberately: these must be
// the bytes Verify authenticated. Handing this function a path would reopen
// the swap-between-reads window that returning the buffer closes.
func Restore(snapshot []byte, dbPath string, verify func(candidate string) error) error {
	candidate := dbPath + ".restore.tmp"
	if err := os.WriteFile(candidate, snapshot, 0o600); err != nil {
		return err
	}
	defer os.Remove(candidate)
	if verify != nil {
		if err := verify(candidate); err != nil {
			return fmt.Errorf("backup: restored database failed verification: %w", err)
		}
	}

	recovery := dbPath + ".recovery"
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Rename(dbPath, recovery); err != nil {
			return err
		}
	}
	// Stale WAL/SHM from the replaced database must not be replayed into the
	// restored file.
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")
	if err := os.Rename(candidate, dbPath); err != nil {
		// Attempt to roll back to the previous database.
		os.Rename(recovery, dbPath)
		return err
	}
	return nil
}

// VerifyContainerFormat checks structure (not authenticity) without any key,
// so `vault backup verify` can give early feedback even when locked.
func VerifyContainerFormat(path string) (*Info, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := parseContainer(raw)
	if err != nil {
		return nil, err
	}
	return c.info, nil
}

// containerHeaderLen is magic[8] version[4] vaultID[16] createdAtMs[8]
// snapshotLen[8].
const containerHeaderLen = 8 + 4 + 16 + 8 + 8

// container is a structurally valid but not yet authenticated container. Its
// slices alias the input buffer; nothing here proves the bytes are genuine —
// only Verify's HMAC check does that.
type container struct {
	info     *Info
	body     []byte // everything the HMAC covers: header + snapshot
	snapshot []byte // the SQLite bytes within body
	tag      []byte
}

func parseContainer(raw []byte) (*container, error) {
	if len(raw) < containerHeaderLen+sha256.Size || !bytes.Equal(raw[:8], magic) {
		return nil, ErrBadContainer
	}
	r := bytes.NewReader(raw[8:])
	var version uint32
	binary.Read(r, binary.BigEndian, &version)
	if version != containerVersion {
		return nil, ErrBadContainer
	}
	idBytes := make([]byte, 16)
	io.ReadFull(r, idBytes)
	var createdAtMs, snapLen uint64
	binary.Read(r, binary.BigEndian, &createdAtMs)
	binary.Read(r, binary.BigEndian, &snapLen)
	if uint64(len(raw)) != uint64(containerHeaderLen)+snapLen+sha256.Size {
		return nil, ErrBadContainer
	}
	id, err := shared.IDFromBytes(idBytes)
	if err != nil {
		return nil, ErrBadContainer
	}
	bodyLen := uint64(containerHeaderLen) + snapLen
	return &container{
		info:     &Info{VaultID: id, CreatedAtMs: createdAtMs, SnapshotLen: snapLen},
		body:     raw[:bodyLen],
		snapshot: raw[containerHeaderLen:bodyLen],
		tag:      raw[bodyLen:],
	}, nil
}

// DefaultBackupName suggests a timestamped file name.
func DefaultBackupName(dir string, unixMilli int64) string {
	return filepath.Join(dir, fmt.Sprintf("albear-backup-%d.abk", unixMilli))
}
