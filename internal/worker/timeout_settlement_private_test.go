package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestTimedOutAttemptIsPersistedForRetry(t *testing.T) {
	database := openWorkerStore(t)
	fixed := clock.NewFixed(time.Now().UTC())
	job := enqueueWorkerJob(t, database, fixed.Now(), "slow-notify", 3)
	runner := New(database, fixed, "timeout-owner", time.Millisecond, 20*time.Millisecond,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := runner.Register("slow-notify", HandlerFunc(func(ctx context.Context, _ domain.WorkerJob) error {
		<-ctx.Done()
		return ctx.Err()
	})); err != nil {
		t.Fatal(err)
	}

	err := runner.RunOnce(t.Context())
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunOnce error = %v", err)
	}
	stored, err := database.JobByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.JobRetry || stored.LeaseUntil != nil || stored.LeaseOwner != "" {
		t.Fatalf("timed out job was not settled: status=%s owner=%q lease=%v",
			stored.Status, stored.LeaseOwner, stored.LeaseUntil)
	}
	if stored.LastError == "" || stored.Attempts != 1 {
		t.Fatalf("retry evidence missing: attempts=%d error=%q", stored.Attempts, stored.LastError)
	}
}
