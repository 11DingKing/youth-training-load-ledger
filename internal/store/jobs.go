package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (t *Tx) EnqueueJob(ctx context.Context, job domain.WorkerJob) (domain.WorkerJob, error) {
	if job.Kind == "" || job.Payload == "" || job.MaxAttempts < 1 {
		return domain.WorkerJob{}, domain.FieldError{Field: "job", Problem: "kind, payload and attempts are required"}
	}
	result, err := t.tx.ExecContext(ctx, `INSERT INTO worker_jobs(kind, payload_json, status, attempts, max_attempts,
        available_at, lease_owner, lease_until, last_error, created_at, updated_at)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, job.Kind, job.Payload, job.Status, job.Attempts,
		job.MaxAttempts, job.AvailableAt, job.LeaseOwner, job.LeaseUntil, job.LastError, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return domain.WorkerJob{}, fmt.Errorf("enqueue job: %w", err)
	}
	job.ID, err = result.LastInsertId()
	if err != nil {
		return domain.WorkerJob{}, fmt.Errorf("read job id: %w", err)
	}
	return job, nil
}

func (s *Store) ClaimJob(ctx context.Context, owner string, now time.Time, lease time.Duration) (domain.WorkerJob, error) {
	var claimed domain.WorkerJob
	err := s.WithTx(ctx, func(tx *Tx) error {
		var job domain.WorkerJob
		err := tx.tx.QueryRowContext(ctx, `SELECT id, kind, payload_json, status, attempts, max_attempts,
            available_at, lease_owner, lease_until, last_error, created_at, updated_at FROM worker_jobs
            WHERE status IN ('pending','retry','running') AND available_at <= ?
            AND (lease_until IS NULL OR lease_until <= ?) ORDER BY available_at, id LIMIT 1`, now, now).Scan(
			&job.ID, &job.Kind, &job.Payload, &job.Status, &job.Attempts, &job.MaxAttempts,
			&job.AvailableAt, &job.LeaseOwner, &job.LeaseUntil, &job.LastError, &job.CreatedAt, &job.UpdatedAt)
		if err != nil {
			if err == sql.ErrNoRows {
				return domain.ErrNotFound
			}
			return fmt.Errorf("select runnable job: %w", err)
		}
		until := now.Add(lease)
		result, err := tx.tx.ExecContext(ctx, `UPDATE worker_jobs SET status = 'running', lease_owner = ?,
            lease_until = ?, attempts = attempts + 1, updated_at = ? WHERE id = ?
            AND (lease_until IS NULL OR lease_until <= ?)`, owner, until, now, job.ID, now)
		if err != nil {
			return fmt.Errorf("claim job: %w", err)
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return domain.ErrConflict
		}
		job.Status = domain.JobRunning
		job.LeaseOwner = owner
		job.LeaseUntil = &until
		job.Attempts++
		job.UpdatedAt = now
		claimed = job
		return nil
	})
	return claimed, err
}

func (s *Store) CompleteJob(ctx context.Context, id int64, owner string, at time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE worker_jobs SET status = 'succeeded', lease_owner = '',
        lease_until = NULL, updated_at = ? WHERE id = ? AND status = 'running' AND lease_owner = ?`, at, id, owner)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) FailJob(ctx context.Context, job domain.WorkerJob, cause error, at time.Time, backoff time.Duration) error {
	if cause == nil {
		return domain.FieldError{Field: "cause", Problem: "job failure cause is required"}
	}
	status := domain.JobRetry
	available := at.Add(backoff)
	if job.Attempts >= job.MaxAttempts {
		status = domain.JobPermanentFailed
		available = at
	}
	result, err := s.db.ExecContext(ctx, `UPDATE worker_jobs SET status = ?, available_at = ?, lease_owner = '',
        lease_until = NULL, last_error = ?, updated_at = ? WHERE id = ? AND status = 'running'
        AND lease_owner = ?`, status, available, cause.Error(), at, job.ID, job.LeaseOwner)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) SettleFailedAttempt(ctx context.Context, job domain.WorkerJob, cause error, at time.Time, backoff time.Duration) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("settle failed attempt: %w", err)
	}
	return s.FailJob(ctx, job, cause, at, backoff)
}

func (s *Store) JobByID(ctx context.Context, id int64) (domain.WorkerJob, error) {
	var job domain.WorkerJob
	err := s.db.QueryRowContext(ctx, `SELECT id, kind, payload_json, status, attempts, max_attempts,
        available_at, lease_owner, lease_until, last_error, created_at, updated_at FROM worker_jobs WHERE id = ?`, id).Scan(
		&job.ID, &job.Kind, &job.Payload, &job.Status, &job.Attempts, &job.MaxAttempts,
		&job.AvailableAt, &job.LeaseOwner, &job.LeaseUntil, &job.LastError, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return domain.WorkerJob{}, fmt.Errorf("get job: %w", mapSQLError(err))
	}
	return job, nil
}
