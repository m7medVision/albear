// Package sqlite owns the vault database: opening with hardened pragmas,
// checksummed migrations, and the CQRS store wrapper around sqlc-generated
// command and query packages. Only vaultd links this package into a binary
// that opens the real vault file.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) the vault database with the settings required by
// PRD section 17.1: WAL, FULL synchronous, foreign keys, busy timeout, and
// trusted_schema off. The caller is responsible for file permissions.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=trusted_schema(OFF)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	// The daemon is the single writer; one connection removes lock churn.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: ping: %w", err)
	}
	return db, nil
}

// WithTx runs fn inside a transaction, rolling back on any error. Every write
// path in the daemon goes through here (PRD 17.4: every write is transactional).
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
