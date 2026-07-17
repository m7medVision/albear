package sqlite

import (
	"context"
	"database/sql"
	"sync"

	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/command"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/query"
)

// Store is the CQRS access point to the vault database. Reads go through
// Query (plain SELECTs), writes through Command inside a transaction. The
// handle can be swapped atomically for backup restore.
type Store struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) DB() *sql.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db
}

// Query returns the read side bound to the shared connection.
func (s *Store) Query() *query.Queries { return query.New(s.DB()) }

// Command runs fn against the write side inside a single transaction.
func (s *Store) Command(ctx context.Context, fn func(c *command.Queries) error) error {
	return WithTx(ctx, s.DB(), func(tx *sql.Tx) error {
		return fn(command.New(tx))
	})
}

// CommandTx is Command with the transaction handed to fn as well, so a caller
// can read its own uncommitted writes. It exists for the vault-state root
// (internal/catalog), which has to hash the post-mutation catalog and store
// the result in the same transaction — computing it afterwards would leave a
// window where a crash strands a stale root and locks out an untouched vault.
func (s *Store) CommandTx(ctx context.Context, fn func(tx *sql.Tx, c *command.Queries) error) error {
	return WithTx(ctx, s.DB(), func(tx *sql.Tx) error {
		return fn(tx, command.New(tx))
	})
}

// Read runs fn inside a read transaction, giving a consistent view across
// several statements. Verification needs it: the catalog must not shift
// between the rows being hashed and the anchor being read.
func (s *Store) Read(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fn(tx)
}

// Snapshot copies the live database to destPath using VACUUM INTO, which
// produces a transactionally consistent snapshot (PRD 22.2).
func (s *Store) Snapshot(ctx context.Context, destPath string) error {
	_, err := s.DB().ExecContext(ctx, `VACUUM INTO ?`, destPath)
	return err
}

// Swap closes the current handle and installs a new one (backup restore).
func (s *Store) Swap(db *sql.DB) {
	s.mu.Lock()
	old := s.db
	s.db = db
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

// Close closes the underlying handle.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}
