package store

import (
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func enqueueTestJob(t *testing.T, database *Store, now time.Time, maxAttempts int) domain.WorkerJob {
	t.Helper()
	var job domain.WorkerJob
	err := database.WithTx(t.Context(), func(tx *Tx) error {
		var err error
		job, err = tx.EnqueueJob(t.Context(), domain.WorkerJob{Kind: "notify", Payload: `{}`,
			Status: domain.JobPending, MaxAttempts: maxAttempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestClaimAndCompleteJob(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	created := enqueueTestJob(t, database, now, 3)
	claimed, err := database.ClaimJob(t.Context(), "worker-a", now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != created.ID || claimed.Status != domain.JobRunning || claimed.Attempts != 1 || claimed.LeaseUntil == nil {
		t.Fatalf("claimed job = %+v", claimed)
	}
	if _, err = database.ClaimJob(t.Context(), "worker-b", now, 30*time.Second); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("leased job claimed twice: %v", err)
	}
	if err = database.CompleteJob(t.Context(), claimed.ID, "worker-b", now); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("wrong owner complete error = %v", err)
	}
	if err = database.CompleteJob(t.Context(), claimed.ID, "worker-a", now); err != nil {
		t.Fatal(err)
	}
	stored, err := database.JobByID(t.Context(), claimed.ID)
	if err != nil || stored.Status != domain.JobSucceeded || stored.LeaseUntil != nil {
		t.Fatalf("completed job = %+v, %v", stored, err)
	}
}

func TestExpiredLeaseCanBeRecoveredAfterRestart(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	enqueueTestJob(t, database, now, 3)
	first, err := database.ClaimJob(t.Context(), "crashed-worker", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.ClaimJob(t.Context(), "recovery-worker", now.Add(500*time.Millisecond), time.Second); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("live lease reclaimed: %v", err)
	}
	recovered, err := database.ClaimJob(t.Context(), "recovery-worker", now.Add(2*time.Second), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != first.ID || recovered.Attempts != 2 || recovered.LeaseOwner != "recovery-worker" {
		t.Fatalf("recovered job = %+v", recovered)
	}
}

func TestFailJobRetriesThenPermanentlyFails(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	enqueueTestJob(t, database, now, 2)
	first, err := database.ClaimJob(t.Context(), "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("temporary downstream failure")
	if err = database.FailJob(t.Context(), first, cause, now, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	stored, err := database.JobByID(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.JobRetry || stored.LastError != cause.Error() || !stored.AvailableAt.Equal(now.Add(5*time.Second)) {
		t.Fatalf("retry job = %+v", stored)
	}
	second, err := database.ClaimJob(t.Context(), "worker", now.Add(5*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.FailJob(t.Context(), second, errors.New("still failing"), now.Add(6*time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
	stored, err = database.JobByID(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.JobPermanentFailed || stored.Attempts != 2 {
		t.Fatalf("permanent failure = %+v", stored)
	}
}

func TestJobValidationAndOwnership(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	err := database.WithTx(t.Context(), func(tx *Tx) error {
		_, err := tx.EnqueueJob(t.Context(), domain.WorkerJob{Status: domain.JobPending, AvailableAt: now})
		return err
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid job error = %v", err)
	}
	job := enqueueTestJob(t, database, now, 1)
	claimed, err := database.ClaimJob(t.Context(), "owner", now, time.Minute)
	if err != nil || claimed.ID != job.ID {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	if err = database.FailJob(t.Context(), claimed, nil, now, time.Second); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("nil cause error = %v", err)
	}
	wrong := claimed
	wrong.LeaseOwner = "other"
	if err = database.FailJob(t.Context(), wrong, errors.New("failure"), now, time.Second); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("wrong owner failure error = %v", err)
	}
}
