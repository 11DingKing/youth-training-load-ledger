package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
)

func openWorkerStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func enqueueWorkerJob(t *testing.T, database *store.Store, now time.Time, kind string, attempts int) domain.WorkerJob {
	t.Helper()
	var job domain.WorkerJob
	err := database.WithTx(t.Context(), func(tx *store.Tx) error {
		var err error
		job, err = tx.EnqueueJob(t.Context(), domain.WorkerJob{Kind: kind, Payload: `{"id":1}`,
			Status: domain.JobPending, MaxAttempts: attempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func newRunner(database *store.Store, fixed *clock.Fixed) *Runner {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(database, fixed, "worker-test", time.Millisecond, time.Minute, logger)
}

func TestRunOnceCompletesHandledJob(t *testing.T) {
	database := openWorkerStore(t)
	fixed := clock.NewFixed(time.Now().UTC())
	job := enqueueWorkerJob(t, database, fixed.Now(), "notify", 3)
	runner := newRunner(database, fixed)
	var handled domain.WorkerJob
	if err := runner.Register("notify", HandlerFunc(func(ctx context.Context, candidate domain.WorkerJob) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		handled = candidate
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if handled.ID != job.ID || handled.Attempts != 1 {
		t.Fatalf("handled job = %+v", handled)
	}
	stored, err := database.JobByID(t.Context(), job.ID)
	if err != nil || stored.Status != domain.JobSucceeded {
		t.Fatalf("stored job = %+v, %v", stored, err)
	}
}

func TestRunOnceRetriesHandlerError(t *testing.T) {
	database := openWorkerStore(t)
	fixed := clock.NewFixed(time.Now().UTC())
	job := enqueueWorkerJob(t, database, fixed.Now(), "notify", 3)
	runner := newRunner(database, fixed)
	sentinel := errors.New("delivery unavailable")
	if err := runner.Register("notify", HandlerFunc(func(context.Context, domain.WorkerJob) error {
		return sentinel
	})); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	stored, err := database.JobByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.JobRetry || stored.Attempts != 1 || stored.LastError == "" {
		t.Fatalf("retried job = %+v", stored)
	}
	if !stored.AvailableAt.After(fixed.Now()) {
		t.Fatalf("retry was not delayed: %s", stored.AvailableAt)
	}
}

func TestRunOncePermanentlyFailsUnknownKind(t *testing.T) {
	database := openWorkerStore(t)
	fixed := clock.NewFixed(time.Now().UTC())
	job := enqueueWorkerJob(t, database, fixed.Now(), "unregistered", 1)
	runner := newRunner(database, fixed)
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	stored, err := database.JobByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.JobPermanentFailed || stored.LastError == "" {
		t.Fatalf("unknown job = %+v", stored)
	}
}

func TestRegisterRejectsInvalidAndDuplicateHandlers(t *testing.T) {
	database := openWorkerStore(t)
	runner := newRunner(database, clock.NewFixed(time.Now()))
	handler := HandlerFunc(func(context.Context, domain.WorkerJob) error { return nil })
	if err := runner.Register("", handler); err == nil {
		t.Fatal("empty kind accepted")
	}
	if err := runner.Register("notify", nil); err == nil {
		t.Fatal("nil handler accepted")
	}
	if err := runner.Register("notify", handler); err != nil {
		t.Fatal(err)
	}
	if err := runner.Register("notify", handler); err == nil {
		t.Fatal("duplicate handler accepted")
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	database := openWorkerStore(t)
	runner := newRunner(database, clock.NewFixed(time.Now()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := runner.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run canceled error = %v", err)
	}
}

func TestHandlerReceivesLeaseDeadline(t *testing.T) {
	database := openWorkerStore(t)
	fixed := clock.NewFixed(time.Now().UTC())
	enqueueWorkerJob(t, database, fixed.Now(), "deadline", 1)
	runner := newRunner(database, fixed)
	var deadline time.Time
	if err := runner.Register("deadline", HandlerFunc(func(ctx context.Context, _ domain.WorkerJob) error {
		var ok bool
		deadline, ok = ctx.Deadline()
		if !ok {
			return errors.New("missing lease deadline")
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if deadline.Before(before.Add(50 * time.Second)) {
		t.Fatalf("handler deadline too short: %s", deadline)
	}
}
