package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (t *Tx) CreateAthlete(ctx context.Context, athlete domain.Athlete) (domain.Athlete, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO athletes(student_user_id, guardian_user_id, coach_user_id,
        advisor_user_id, birth_date, timezone, status, version, paused_reason, created_at, updated_at)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, athlete.StudentUserID, athlete.GuardianUserID,
		athlete.CoachUserID, athlete.AdvisorUserID, athlete.BirthDate, athlete.Timezone, athlete.Status,
		athlete.Version, athlete.PausedReason, athlete.CreatedAt, athlete.UpdatedAt)
	if err != nil {
		return domain.Athlete{}, fmt.Errorf("create athlete: %w", mapSQLError(err))
	}
	athlete.ID, err = result.LastInsertId()
	if err != nil {
		return domain.Athlete{}, fmt.Errorf("read athlete id: %w", err)
	}
	return athlete, nil
}

func (s *Store) AthleteByID(ctx context.Context, id int64) (domain.Athlete, error) {
	return athleteByID(ctx, s.db, id)
}

func (t *Tx) AthleteByID(ctx context.Context, id int64) (domain.Athlete, error) {
	return athleteByID(ctx, t.tx, id)
}

func athleteByID(ctx context.Context, exec Executor, id int64) (domain.Athlete, error) {
	var a domain.Athlete
	err := exec.QueryRowContext(ctx, `SELECT id, student_user_id, guardian_user_id, coach_user_id, advisor_user_id,
        birth_date, timezone, status, version, paused_reason, created_at, updated_at FROM athletes WHERE id = ?`, id).Scan(
		&a.ID, &a.StudentUserID, &a.GuardianUserID, &a.CoachUserID, &a.AdvisorUserID, &a.BirthDate,
		&a.Timezone, &a.Status, &a.Version, &a.PausedReason, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return domain.Athlete{}, fmt.Errorf("get athlete: %w", mapSQLError(err))
	}
	return a, nil
}

func (t *Tx) UpdateAthlete(ctx context.Context, athlete domain.Athlete, expectedVersion int64) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE athletes SET status = ?, version = ?, paused_reason = ?,
        coach_user_id = ?, advisor_user_id = ?, updated_at = ? WHERE id = ? AND version = ?`,
		athlete.Status, athlete.Version, athlete.PausedReason, athlete.CoachUserID, athlete.AdvisorUserID,
		athlete.UpdatedAt, athlete.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update athlete: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("athlete update count: %w", err)
	}
	if count != 1 {
		return domain.ErrVersionConflict
	}
	return nil
}

func (t *Tx) CreateConsent(ctx context.Context, consent domain.GuardianConsent) (domain.GuardianConsent, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO guardian_consents(athlete_id, guardian_id, status, terms_version,
        effective_at, expires_at, withdrawn_at, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, consent.AthleteID,
		consent.GuardianID, consent.Status, consent.TermsVersion, consent.EffectiveAt, consent.ExpiresAt,
		consent.WithdrawnAt, consent.CreatedAt)
	if err != nil {
		return domain.GuardianConsent{}, fmt.Errorf("create consent: %w", mapSQLError(err))
	}
	consent.ID, err = result.LastInsertId()
	if err != nil {
		return domain.GuardianConsent{}, fmt.Errorf("read consent id: %w", err)
	}
	return consent, nil
}

func (t *Tx) CurrentConsent(ctx context.Context, athleteID int64) (domain.GuardianConsent, error) {
	var c domain.GuardianConsent
	err := t.tx.QueryRowContext(ctx, `SELECT id, athlete_id, guardian_id, status, terms_version, effective_at,
        expires_at, withdrawn_at, created_at FROM guardian_consents WHERE athlete_id = ?
        ORDER BY id DESC LIMIT 1`, athleteID).Scan(&c.ID, &c.AthleteID, &c.GuardianID, &c.Status,
		&c.TermsVersion, &c.EffectiveAt, &c.ExpiresAt, &c.WithdrawnAt, &c.CreatedAt)
	if err != nil {
		return domain.GuardianConsent{}, fmt.Errorf("get consent: %w", mapSQLError(err))
	}
	return c, nil
}

func (t *Tx) UpdateConsent(ctx context.Context, consent domain.GuardianConsent) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE guardian_consents SET status = ?, effective_at = ?, expires_at = ?,
        withdrawn_at = ? WHERE id = ?`, consent.Status, consent.EffectiveAt, consent.ExpiresAt, consent.WithdrawnAt, consent.ID)
	if err != nil {
		return fmt.Errorf("update consent: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (t *Tx) InsertTransition(ctx context.Context, transition domain.StatusTransition) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO status_transitions(athlete_id, actor_id, from_status, to_status,
        reason, request_id, occurred_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, transition.AthleteID, transition.ActorID,
		transition.FromStatus, transition.ToStatus, transition.Reason, transition.RequestID, transition.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert status transition: %w", err)
	}
	return nil
}

func (s *Store) ListAthletes(ctx context.Context, user domain.User, limit, offset int) ([]domain.Athlete, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, domain.FieldError{Field: "pagination", Problem: "invalid limit or offset"}
	}
	query := `SELECT id, student_user_id, guardian_user_id, coach_user_id, advisor_user_id, birth_date,
        timezone, status, version, paused_reason, created_at, updated_at FROM athletes`
	args := []any{}
	switch user.Role {
	case domain.RoleStudent:
		query += ` WHERE student_user_id = ?`
		args = append(args, user.ID)
	case domain.RoleGuardian:
		query += ` WHERE guardian_user_id = ?`
		args = append(args, user.ID)
	case domain.RoleCoach:
		query += ` WHERE coach_user_id = ?`
		args = append(args, user.ID)
	case domain.RoleAdvisor:
		query += ` WHERE advisor_user_id = ? OR advisor_user_id IS NULL`
		args = append(args, user.ID)
	default:
		return nil, domain.ErrForbidden
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list athletes: %w", err)
	}
	defer rows.Close()
	athletes := make([]domain.Athlete, 0)
	for rows.Next() {
		var a domain.Athlete
		if err = rows.Scan(&a.ID, &a.StudentUserID, &a.GuardianUserID, &a.CoachUserID, &a.AdvisorUserID,
			&a.BirthDate, &a.Timezone, &a.Status, &a.Version, &a.PausedReason, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan athlete: %w", err)
		}
		athletes = append(athletes, a)
	}
	if err = rows.Err(); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("iterate athletes: %w", err)
	}
	return athletes, nil
}
