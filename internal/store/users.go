package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (s *Store) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	return createUser(ctx, s.db, user)
}

func (t *Tx) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	return createUser(ctx, t.tx, user)
}

func createUser(ctx context.Context, exec Executor, user domain.User) (domain.User, error) {
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	user.DisplayName = strings.TrimSpace(user.DisplayName)
	if user.Email == "" || !strings.Contains(user.Email, "@") {
		return domain.User{}, domain.FieldError{Field: "email", Problem: "valid email is required"}
	}
	if user.DisplayName == "" || user.PasswordHash == "" {
		return domain.User{}, domain.FieldError{Field: "user", Problem: "display name and password hash are required"}
	}
	if _, err := domain.ParseRole(string(user.Role)); err != nil {
		return domain.User{}, err
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now().UTC()
	}
	user.UpdatedAt = user.CreatedAt
	user.Active = true
	result, err := exec.ExecContext(ctx, `INSERT INTO users(email, display_name, role, password_hash, active, created_at, updated_at)
        VALUES(?, ?, ?, ?, 1, ?, ?)`, user.Email, user.DisplayName, user.Role, user.PasswordHash, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", mapSQLError(err))
	}
	user.ID, err = result.LastInsertId()
	if err != nil {
		return domain.User{}, fmt.Errorf("read user id: %w", err)
	}
	return user, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return userBy(ctx, s.db, `SELECT id, email, display_name, role, password_hash, active, created_at, updated_at
        FROM users WHERE email = ? COLLATE NOCASE`, strings.TrimSpace(email))
}

func (s *Store) UserByID(ctx context.Context, id int64) (domain.User, error) {
	return userBy(ctx, s.db, `SELECT id, email, display_name, role, password_hash, active, created_at, updated_at
        FROM users WHERE id = ?`, id)
}

func (t *Tx) UserByID(ctx context.Context, id int64) (domain.User, error) {
	return userBy(ctx, t.tx, `SELECT id, email, display_name, role, password_hash, active, created_at, updated_at
        FROM users WHERE id = ?`, id)
}

func userBy(ctx context.Context, exec Executor, query string, arg any) (domain.User, error) {
	var user domain.User
	var active int
	err := exec.QueryRowContext(ctx, query, arg).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role,
		&user.PasswordHash, &active, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return domain.User{}, fmt.Errorf("get user: %w", mapSQLError(err))
	}
	user.Active = active == 1
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, session domain.Session) (domain.Session, error) {
	if session.UserID <= 0 || session.TokenHash == "" || !session.ExpiresAt.After(session.CreatedAt) {
		return domain.Session{}, domain.FieldError{Field: "session", Problem: "invalid session lifecycle"}
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO sessions(user_id, token_hash, expires_at, created_at)
        VALUES(?, ?, ?, ?)`, session.UserID, session.TokenHash, session.ExpiresAt, session.CreatedAt)
	if err != nil {
		return domain.Session{}, fmt.Errorf("create session: %w", mapSQLError(err))
	}
	session.ID, err = result.LastInsertId()
	if err != nil {
		return domain.Session{}, fmt.Errorf("read session id: %w", err)
	}
	return session, nil
}

func (s *Store) SessionUser(ctx context.Context, tokenHash string) (domain.Session, domain.User, error) {
	var session domain.Session
	var user domain.User
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT s.id, s.user_id, s.token_hash, s.expires_at, s.revoked_at, s.created_at,
        u.id, u.email, u.display_name, u.role, u.password_hash, u.active, u.created_at, u.updated_at
        FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, tokenHash).Scan(
		&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt, &session.RevokedAt, &session.CreatedAt,
		&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.PasswordHash, &active, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return domain.Session{}, domain.User{}, fmt.Errorf("get session: %w", mapSQLError(err))
	}
	user.Active = active == 1
	return session, user, nil
}

func (s *Store) SessionUserForAuthentication(ctx context.Context, tokenHash string) (domain.Session, domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	return s.SessionUser(ctx, tokenHash)
}

func (s *Store) RevokeSession(ctx context.Context, tokenHash string, at time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ?
        WHERE token_hash = ? AND revoked_at IS NULL`, at, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("session revoke count: %w", err)
	}
	if rows == 0 {
		var revoked sql.NullTime
		err = s.db.QueryRowContext(ctx, `SELECT revoked_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&revoked)
		if err != nil {
			return fmt.Errorf("find session to revoke: %w", mapSQLError(err))
		}
	}
	return nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ? OR revoked_at IS NOT NULL`, before)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expired session count: %w", err)
	}
	return count, nil
}
