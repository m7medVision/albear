package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies embedded migrations in order inside transactions, recording
// a checksum for each. Re-running verifies checksums: a mismatch means the
// migration history was tampered with or the binary is inconsistent with the
// database, and the daemon must refuse to proceed (PRD 17.4).
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version       INTEGER PRIMARY KEY,
		applied_at_ms INTEGER NOT NULL,
		checksum      BLOB NOT NULL CHECK(length(checksum) = 32)
	) STRICT`); err != nil {
		return fmt.Errorf("sqlite: create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)

		var stored []byte
		err = db.QueryRowContext(ctx,
			`SELECT checksum FROM schema_migrations WHERE version = ?`, version).Scan(&stored)
		switch err {
		case nil:
			if string(stored) != string(sum[:]) {
				return fmt.Errorf("sqlite: migration %d checksum mismatch", version)
			}
			continue
		case sql.ErrNoRows:
			// apply below
		default:
			return err
		}

		if err := WithTx(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("sqlite: apply migration %d: %w", version, err)
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations (version, applied_at_ms, checksum) VALUES (?, ?, ?)`,
				version, time.Now().UnixMilli(), sum[:])
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the highest applied migration version.
func SchemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var v sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, err
	}
	return v.Int64, nil
}

func migrationVersion(name string) (int64, error) {
	base, ok := strings.CutSuffix(name, ".sql")
	if !ok {
		return 0, fmt.Errorf("sqlite: migration %q is not a .sql file", name)
	}
	prefix, _, _ := strings.Cut(base, "_")
	v, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("sqlite: migration %q has no numeric version prefix", name)
	}
	return v, nil
}
