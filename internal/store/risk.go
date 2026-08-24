package store

import (
	"context"
	"fmt"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (t *Tx) CreateFatigue(ctx context.Context, report domain.FatigueReport) (domain.FatigueReport, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO fatigue_reports(athlete_id, reporter_user_id, reported_for,
        fatigue_score, soreness_score, sleep_hours, notes, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		report.AthleteID, report.ReporterUserID, report.ReportedFor, report.FatigueScore, report.SorenessScore,
		report.SleepHours, report.Notes, report.CreatedAt)
	if err != nil {
		return domain.FatigueReport{}, fmt.Errorf("create fatigue report: %w", mapSQLError(err))
	}
	report.ID, err = result.LastInsertId()
	if err != nil {
		return domain.FatigueReport{}, fmt.Errorf("read fatigue report id: %w", err)
	}
	return report, nil
}

func (t *Tx) CreateRisk(ctx context.Context, risk domain.RiskCase) (domain.RiskCase, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO risk_cases(athlete_id, trigger_type, trigger_reference_id,
        severity, status, basis, assigned_advisor_id, resolution, opened_at, acknowledged_at, resolved_at, version)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, risk.AthleteID, risk.TriggerType, risk.TriggerReferenceID,
		risk.Severity, risk.Status, risk.Basis, risk.AssignedAdvisorID, risk.Resolution, risk.OpenedAt,
		risk.AcknowledgedAt, risk.ResolvedAt, risk.Version)
	if err != nil {
		return domain.RiskCase{}, fmt.Errorf("create risk case: %w", mapSQLError(err))
	}
	risk.ID, err = result.LastInsertId()
	if err != nil {
		return domain.RiskCase{}, fmt.Errorf("read risk id: %w", err)
	}
	return risk, nil
}

func (s *Store) RiskByID(ctx context.Context, id int64) (domain.RiskCase, error) {
	return riskByID(ctx, s.db, id)
}

func (t *Tx) RiskByID(ctx context.Context, id int64) (domain.RiskCase, error) {
	return riskByID(ctx, t.tx, id)
}

func riskByID(ctx context.Context, exec Executor, id int64) (domain.RiskCase, error) {
	var risk domain.RiskCase
	err := exec.QueryRowContext(ctx, `SELECT id, athlete_id, trigger_type, trigger_reference_id, severity, status,
        basis, assigned_advisor_id, resolution, opened_at, acknowledged_at, resolved_at, version
        FROM risk_cases WHERE id = ?`, id).Scan(&risk.ID, &risk.AthleteID, &risk.TriggerType,
		&risk.TriggerReferenceID, &risk.Severity, &risk.Status, &risk.Basis, &risk.AssignedAdvisorID,
		&risk.Resolution, &risk.OpenedAt, &risk.AcknowledgedAt, &risk.ResolvedAt, &risk.Version)
	if err != nil {
		return domain.RiskCase{}, fmt.Errorf("get risk case: %w", mapSQLError(err))
	}
	return risk, nil
}

func (t *Tx) UpdateRisk(ctx context.Context, risk domain.RiskCase, expectedVersion int64) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE risk_cases SET status = ?, assigned_advisor_id = ?, resolution = ?,
        acknowledged_at = ?, resolved_at = ?, version = ? WHERE id = ? AND version = ?`, risk.Status,
		risk.AssignedAdvisorID, risk.Resolution, risk.AcknowledgedAt, risk.ResolvedAt, risk.Version,
		risk.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update risk case: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrVersionConflict
	}
	return nil
}

func (t *Tx) CountOpenRisks(ctx context.Context, athleteID int64) (int, error) {
	var count int
	err := t.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_cases WHERE athlete_id = ?
        AND status IN ('open','acknowledged')`, athleteID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count open risks: %w", err)
	}
	return count, nil
}

func (s *Store) ListRisks(ctx context.Context, athleteID int64, status domain.RiskStatus, limit int) ([]domain.RiskCase, error) {
	if limit < 1 || limit > 100 {
		return nil, domain.FieldError{Field: "limit", Problem: "must be between 1 and 100"}
	}
	query := `SELECT id, athlete_id, trigger_type, trigger_reference_id, severity, status, basis,
        assigned_advisor_id, resolution, opened_at, acknowledged_at, resolved_at, version
        FROM risk_cases WHERE athlete_id = ?`
	args := []any{athleteID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY opened_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list risks: %w", err)
	}
	defer rows.Close()
	items := make([]domain.RiskCase, 0)
	for rows.Next() {
		var risk domain.RiskCase
		if err = rows.Scan(&risk.ID, &risk.AthleteID, &risk.TriggerType, &risk.TriggerReferenceID,
			&risk.Severity, &risk.Status, &risk.Basis, &risk.AssignedAdvisorID, &risk.Resolution,
			&risk.OpenedAt, &risk.AcknowledgedAt, &risk.ResolvedAt, &risk.Version); err != nil {
			return nil, fmt.Errorf("scan risk: %w", err)
		}
		items = append(items, risk)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate risks: %w", err)
	}
	return items, nil
}
