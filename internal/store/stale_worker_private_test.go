package store

import (
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestStaleWorkerCannotCompleteReclaimedJob(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	var job domain.WorkerJob
	if err := database.WithTx(t.Context(), func(tx *Tx) error {
		var err error
		job, err = tx.EnqueueJob(t.Context(), domain.WorkerJob{Kind: "risk_notification",
			Payload: `{"risk":1}`, Status: domain.JobPending, MaxAttempts: 3,
			AvailableAt: now, CreatedAt: now, UpdatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	first, err := database.ClaimJob(t.Context(), "owner-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.ClaimJob(t.Context(), "owner-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != job.ID || second.ID != job.ID {
		t.Fatalf("claims targeted different jobs: first=%d second=%d want=%d", first.ID, second.ID, job.ID)
	}
	if err = database.CompleteJob(t.Context(), job.ID, "owner-a", now.Add(2*time.Minute)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale completion error = %v, want conflict", err)
	}
	stored, err := database.JobByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.JobRunning || stored.LeaseOwner != "owner-b" {
		t.Fatalf("stale owner replaced current lease: status=%s owner=%q", stored.Status, stored.LeaseOwner)
	}
}
