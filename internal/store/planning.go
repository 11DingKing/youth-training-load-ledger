package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (t *Tx) CreateScreening(ctx context.Context, screening domain.HealthScreening) (domain.HealthScreening, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO health_screenings(athlete_id, answers_json, decision,
        reviewer_user_id, review_basis, submitted_at, reviewed_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, screening.AthleteID,
		screening.AnswersJSON, screening.Decision, screening.ReviewerUserID, screening.ReviewBasis,
		screening.SubmittedAt, screening.ReviewedAt)
	if err != nil {
		return domain.HealthScreening{}, fmt.Errorf("create screening: %w", mapSQLError(err))
	}
	screening.ID, err = result.LastInsertId()
	if err != nil {
		return domain.HealthScreening{}, fmt.Errorf("read screening id: %w", err)
	}
	return screening, nil
}

func (t *Tx) LatestScreening(ctx context.Context, athleteID int64) (domain.HealthScreening, error) {
	var screening domain.HealthScreening
	err := t.tx.QueryRowContext(ctx, `SELECT id, athlete_id, answers_json, decision, reviewer_user_id,
        review_basis, submitted_at, reviewed_at FROM health_screenings WHERE athlete_id = ?
        ORDER BY submitted_at DESC, id DESC LIMIT 1`, athleteID).Scan(&screening.ID, &screening.AthleteID,
		&screening.AnswersJSON, &screening.Decision, &screening.ReviewerUserID, &screening.ReviewBasis,
		&screening.SubmittedAt, &screening.ReviewedAt)
	if err != nil {
		return domain.HealthScreening{}, fmt.Errorf("get screening: %w", mapSQLError(err))
	}
	return screening, nil
}

func (t *Tx) UpdateScreening(ctx context.Context, screening domain.HealthScreening) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE health_screenings SET decision = ?, reviewer_user_id = ?,
        review_basis = ?, reviewed_at = ? WHERE id = ?`, screening.Decision, screening.ReviewerUserID,
		screening.ReviewBasis, screening.ReviewedAt, screening.ID)
	if err != nil {
		return fmt.Errorf("update screening: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (t *Tx) CreateBaseline(ctx context.Context, baseline domain.BaselineAssessment) (domain.BaselineAssessment, error) {
	var sequence int
	if err := t.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM baseline_assessments
        WHERE athlete_id = ?`, baseline.AthleteID).Scan(&sequence); err != nil {
		return domain.BaselineAssessment{}, fmt.Errorf("next baseline sequence: %w", err)
	}
	baseline.Sequence = sequence
	result, err := t.tx.ExecContext(ctx, `INSERT INTO baseline_assessments(athlete_id, assessor_user_id, sequence,
        endurance_score, strength_score, mobility_score, resting_heart_rate, conclusion, assessed_at)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, baseline.AthleteID, baseline.AssessorUserID, baseline.Sequence,
		baseline.EnduranceScore, baseline.StrengthScore, baseline.MobilityScore, baseline.RestingHeartRate,
		baseline.Conclusion, baseline.AssessedAt)
	if err != nil {
		return domain.BaselineAssessment{}, fmt.Errorf("create baseline: %w", mapSQLError(err))
	}
	baseline.ID, err = result.LastInsertId()
	if err != nil {
		return domain.BaselineAssessment{}, fmt.Errorf("read baseline id: %w", err)
	}
	return baseline, nil
}

func (t *Tx) LatestBaseline(ctx context.Context, athleteID int64) (domain.BaselineAssessment, error) {
	var baseline domain.BaselineAssessment
	err := t.tx.QueryRowContext(ctx, `SELECT id, athlete_id, assessor_user_id, sequence, endurance_score,
        strength_score, mobility_score, resting_heart_rate, conclusion, assessed_at FROM baseline_assessments
        WHERE athlete_id = ? ORDER BY assessed_at DESC, id DESC LIMIT 1`, athleteID).Scan(&baseline.ID,
		&baseline.AthleteID, &baseline.AssessorUserID, &baseline.Sequence, &baseline.EnduranceScore,
		&baseline.StrengthScore, &baseline.MobilityScore, &baseline.RestingHeartRate, &baseline.Conclusion,
		&baseline.AssessedAt)
	if err != nil {
		return domain.BaselineAssessment{}, fmt.Errorf("get baseline: %w", mapSQLError(err))
	}
	return baseline, nil
}

func (t *Tx) CreatePrescription(ctx context.Context, prescription domain.Prescription) (domain.Prescription, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO prescriptions(athlete_id, author_user_id, week_start, version,
        status, weekly_load_limit, max_session_load, min_recovery_hours, strength_days, basis, published_at,
        superseded_at, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, prescription.AthleteID,
		prescription.AuthorUserID, prescription.WeekStart, prescription.Version, prescription.Status,
		prescription.WeeklyLoadLimit, prescription.MaxSessionLoad, prescription.MinRecoveryHours,
		prescription.StrengthDays, prescription.Basis, prescription.PublishedAt, prescription.SupersededAt,
		prescription.CreatedAt)
	if err != nil {
		return domain.Prescription{}, fmt.Errorf("create prescription: %w", mapSQLError(err))
	}
	prescription.ID, err = result.LastInsertId()
	if err != nil {
		return domain.Prescription{}, fmt.Errorf("read prescription id: %w", err)
	}
	return prescription, nil
}

func (t *Tx) PrescriptionByID(ctx context.Context, id int64) (domain.Prescription, error) {
	return prescriptionByID(ctx, t.tx, id)
}

func (s *Store) PrescriptionByID(ctx context.Context, id int64) (domain.Prescription, error) {
	return prescriptionByID(ctx, s.db, id)
}

func prescriptionByID(ctx context.Context, exec Executor, id int64) (domain.Prescription, error) {
	var p domain.Prescription
	err := exec.QueryRowContext(ctx, `SELECT id, athlete_id, author_user_id, week_start, version, status,
        weekly_load_limit, max_session_load, min_recovery_hours, strength_days, basis, published_at,
        superseded_at, created_at FROM prescriptions WHERE id = ?`, id).Scan(&p.ID, &p.AthleteID,
		&p.AuthorUserID, &p.WeekStart, &p.Version, &p.Status, &p.WeeklyLoadLimit, &p.MaxSessionLoad,
		&p.MinRecoveryHours, &p.StrengthDays, &p.Basis, &p.PublishedAt, &p.SupersededAt, &p.CreatedAt)
	if err != nil {
		return domain.Prescription{}, fmt.Errorf("get prescription: %w", mapSQLError(err))
	}
	return p, nil
}

func (t *Tx) PublishedPrescription(ctx context.Context, athleteID int64, weekStart any) (domain.Prescription, error) {
	var p domain.Prescription
	err := t.tx.QueryRowContext(ctx, `SELECT id, athlete_id, author_user_id, week_start, version, status,
        weekly_load_limit, max_session_load, min_recovery_hours, strength_days, basis, published_at,
        superseded_at, created_at FROM prescriptions WHERE athlete_id = ? AND week_start = ? AND status = 'published'`,
		athleteID, weekStart).Scan(&p.ID, &p.AthleteID, &p.AuthorUserID, &p.WeekStart, &p.Version,
		&p.Status, &p.WeeklyLoadLimit, &p.MaxSessionLoad, &p.MinRecoveryHours, &p.StrengthDays, &p.Basis,
		&p.PublishedAt, &p.SupersededAt, &p.CreatedAt)
	if err != nil {
		return domain.Prescription{}, fmt.Errorf("get published prescription: %w", mapSQLError(err))
	}
	return p, nil
}

func (s *Store) SupersedePublishedPrescription(ctx context.Context, athleteID int64, weekStart any, at time.Time) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		current, err := tx.PublishedPrescription(ctx, athleteID, weekStart)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		expectedVersion := current.Version
		if err = current.Supersede(at); err != nil {
			return err
		}
		return tx.UpdatePrescription(ctx, current, expectedVersion)
	})
}

func (t *Tx) UpdatePrescription(ctx context.Context, prescription domain.Prescription, expectedVersion int64) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE prescriptions SET status = ?, version = ?, published_at = ?,
        superseded_at = ? WHERE id = ? AND version = ?`, prescription.Status, prescription.Version,
		prescription.PublishedAt, prescription.SupersededAt, prescription.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update prescription: %w", mapSQLError(err))
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrVersionConflict
	}
	return nil
}

func (t *Tx) CreateStrengthBlock(ctx context.Context, block domain.StrengthBlock) (domain.StrengthBlock, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO strength_blocks(prescription_id, day_offset, muscle_group,
        sets, repetitions, intensity_rpe, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, block.PrescriptionID,
		block.DayOffset, block.MuscleGroup, block.Sets, block.Repetitions, block.IntensityRPE, block.CreatedAt)
	if err != nil {
		return domain.StrengthBlock{}, fmt.Errorf("create strength block: %w", mapSQLError(err))
	}
	block.ID, err = result.LastInsertId()
	if err != nil {
		return domain.StrengthBlock{}, fmt.Errorf("read strength block id: %w", err)
	}
	return block, nil
}

func (t *Tx) CreateReassessment(ctx context.Context, item domain.Reassessment) (domain.Reassessment, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO reassessments(athlete_id, assessor_user_id, baseline_id,
        endurance_score, strength_score, mobility_score, recommendation, basis, assessed_at)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.AthleteID, item.AssessorUserID, item.BaselineID,
		item.EnduranceScore, item.StrengthScore, item.MobilityScore, item.Recommendation, item.Basis, item.AssessedAt)
	if err != nil {
		return domain.Reassessment{}, fmt.Errorf("create reassessment: %w", mapSQLError(err))
	}
	item.ID, err = result.LastInsertId()
	if err != nil {
		return domain.Reassessment{}, fmt.Errorf("read reassessment id: %w", err)
	}
	return item, nil
}

func (t *Tx) LatestReassessment(ctx context.Context, athleteID int64) (domain.Reassessment, error) {
	var item domain.Reassessment
	err := t.tx.QueryRowContext(ctx, `SELECT id, athlete_id, assessor_user_id, baseline_id, endurance_score,
        strength_score, mobility_score, recommendation, basis, assessed_at FROM reassessments
        WHERE athlete_id = ? ORDER BY assessed_at DESC, id DESC LIMIT 1`, athleteID).Scan(&item.ID,
		&item.AthleteID, &item.AssessorUserID, &item.BaselineID, &item.EnduranceScore, &item.StrengthScore,
		&item.MobilityScore, &item.Recommendation, &item.Basis, &item.AssessedAt)
	if err != nil {
		return domain.Reassessment{}, fmt.Errorf("get reassessment: %w", mapSQLError(err))
	}
	return item, nil
}
