package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/migrate"
)

type Store struct {
	db *sql.DB
}

type Tx struct {
	tx *sql.Tx
}

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("database path: %w", domain.ErrInvalid)
	}
	dsn := path
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		dsn = "file:" + path
	}
	if strings.Contains(dsn, "?") {
		dsn += "&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	} else {
		dsn += "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err = db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil && path != ":memory:" {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if err = migrate.Apply(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ready(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database not ready: %w", err)
	}
	version, err := migrate.CurrentVersion(ctx, s.db)
	if err != nil {
		return err
	}
	if version != len(migrate.All) {
		return fmt.Errorf("database schema version %d, want %d", version, len(migrate.All))
	}
	return nil
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	txCtx := transactionCommitContext(ctx)
	tx, err := s.db.BeginTx(txCtx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	wrapper := &Tx{tx: tx}
	if err = fn(wrapper); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("rollback transaction: %w", rollbackErr))
		}
		return err
	}
	commitCtx := transactionCommitContext(ctx)
	if err = commitCtx.Err(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func transactionCommitContext(requestCtx context.Context) context.Context {
	commitCtx := context.WithoutCancel(requestCtx)
	if deadline, ok := requestCtx.Deadline(); ok {
		commitCtx, _ = context.WithDeadline(commitCtx, deadline)
	}
	return commitCtx
}

func (t *Tx) Executor() Executor { return t.tx }

func mapSQLError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique constraint") || strings.Contains(message, "constraint failed") {
		return fmt.Errorf("%w: %v", domain.ErrConflict, err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
