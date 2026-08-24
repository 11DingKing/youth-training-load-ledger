package migrate

import (
	"context"
	"database/sql"
	"fmt"
)

func Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    )`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, migration := range All {
		var name string
		err := db.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE version = ?`, migration.Version).Scan(&name)
		if err == nil {
			if name != migration.Name {
				return fmt.Errorf("migration %d name conflict: database=%q binary=%q", migration.Version, name, migration.Name)
			}
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("inspect migration %d: %w", migration.Version, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.Version, err)
		}
		if _, err = tx.ExecContext(ctx, migration.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d %s: %w", migration.Version, migration.Name, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name) VALUES(?, ?)`, migration.Version, migration.Name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.Version, err)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.Version, err)
		}
	}
	return nil
}

func CurrentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	return version, nil
}
