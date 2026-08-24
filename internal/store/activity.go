package store

import (
	"context"
	"fmt"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (t *Tx) CreateActivity(ctx context.Context, activity domain.ActivityLog) (domain.ActivityLog, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO activity_logs(athlete_id, prescription_id, recorder_user_id,
        idempotency_key, occurred_at, recorded_at, duration_minutes, perceived_effort, load_units, source,
        late_entry, supersedes_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, activity.AthleteID,
		activity.PrescriptionID, activity.RecorderUserID, activity.IdempotencyKey, activity.OccurredAt,
		activity.RecordedAt, activity.DurationMinutes, activity.PerceivedEffort, activity.LoadUnits, activity.Source,
		boolInt(activity.LateEntry), activity.SupersedesID, activity.CreatedAt)
	if err != nil {
		return domain.ActivityLog{}, fmt.Errorf("create activity: %w", mapSQLError(err))
	}
	activity.ID, err = result.LastInsertId()
	if err != nil {
		return domain.ActivityLog{}, fmt.Errorf("read activity id: %w", err)
	}
	return activity, nil
}

func (t *Tx) ActivityByIdempotency(ctx context.Context, athleteID int64, key string) (domain.ActivityLog, error) {
	return scanActivity(t.tx.QueryRowContext(ctx, `SELECT id, athlete_id, prescription_id, recorder_user_id,
        idempotency_key, occurred_at, recorded_at, duration_minutes, perceived_effort, load_units, source,
        late_entry, supersedes_id, created_at FROM activity_logs WHERE athlete_id = ? AND idempotency_key = ?`,
		athleteID, key))
}

func (t *Tx) ActivityByID(ctx context.Context, id int64) (domain.ActivityLog, error) {
	return scanActivity(t.tx.QueryRowContext(ctx, `SELECT id, athlete_id, prescription_id, recorder_user_id,
        idempotency_key, occurred_at, recorded_at, duration_minutes, perceived_effort, load_units, source,
        late_entry, supersedes_id, created_at FROM activity_logs WHERE id = ?`, id))
}

type rowScanner interface{ Scan(...any) error }

func scanActivity(row rowScanner) (domain.ActivityLog, error) {
	var activity domain.ActivityLog
	var late int
	err := row.Scan(&activity.ID, &activity.AthleteID, &activity.PrescriptionID, &activity.RecorderUserID,
		&activity.IdempotencyKey, &activity.OccurredAt, &activity.RecordedAt, &activity.DurationMinutes,
		&activity.PerceivedEffort, &activity.LoadUnits, &activity.Source, &late, &activity.SupersedesID,
		&activity.CreatedAt)
	if err != nil {
		return domain.ActivityLog{}, fmt.Errorf("get activity: %w", mapSQLError(err))
	}
	activity.LateEntry = late == 1
	return activity, nil
}

func (t *Tx) ActivitiesInWindow(ctx context.Context, athleteID int64, start, end time.Time) ([]domain.ActivityLog, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT id, athlete_id, prescription_id, recorder_user_id, idempotency_key,
        occurred_at, recorded_at, duration_minutes, perceived_effort, load_units, source, late_entry, supersedes_id,
        created_at FROM activity_logs WHERE athlete_id = ? AND occurred_at >= ? AND occurred_at <= ?
        ORDER BY occurred_at, id`, athleteID, start, end)
	if err != nil {
		return nil, fmt.Errorf("query activity window: %w", err)
	}
	defer rows.Close()
	items := make([]domain.ActivityLog, 0)
	for rows.Next() {
		item, scanErr := scanActivity(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity window: %w", err)
	}
	return items, nil
}

func (t *Tx) CreateLoadSnapshot(ctx context.Context, snapshot domain.LoadSnapshot) (domain.LoadSnapshot, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO load_snapshots(athlete_id, activity_id, window_start,
        window_end, seven_day_load, previous_load, threshold, risk_triggered, calculated_at)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, snapshot.AthleteID, snapshot.ActivityID, snapshot.WindowStart,
		snapshot.WindowEnd, snapshot.SevenDayLoad, snapshot.PreviousLoad, snapshot.Threshold,
		boolInt(snapshot.RiskTriggered), snapshot.CalculatedAt)
	if err != nil {
		return domain.LoadSnapshot{}, fmt.Errorf("create load snapshot: %w", mapSQLError(err))
	}
	snapshot.ID, err = result.LastInsertId()
	if err != nil {
		return domain.LoadSnapshot{}, fmt.Errorf("read load snapshot id: %w", err)
	}
	return snapshot, nil
}

func (s *Store) LatestLoadSnapshot(ctx context.Context, athleteID int64) (domain.LoadSnapshot, error) {
	var item domain.LoadSnapshot
	var triggered int
	err := s.db.QueryRowContext(ctx, `SELECT id, athlete_id, activity_id, window_start, window_end, seven_day_load,
        previous_load, threshold, risk_triggered, calculated_at FROM load_snapshots WHERE athlete_id = ?
        ORDER BY window_end DESC, id DESC LIMIT 1`, athleteID).Scan(&item.ID, &item.AthleteID, &item.ActivityID,
		&item.WindowStart, &item.WindowEnd, &item.SevenDayLoad, &item.PreviousLoad, &item.Threshold,
		&triggered, &item.CalculatedAt)
	if err != nil {
		return domain.LoadSnapshot{}, fmt.Errorf("get load snapshot: %w", mapSQLError(err))
	}
	item.RiskTriggered = triggered == 1
	return item, nil
}
