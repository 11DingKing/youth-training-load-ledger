package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
)

type Handler interface {
	Handle(context.Context, domain.WorkerJob) error
}

type HandlerFunc func(context.Context, domain.WorkerJob) error

func (f HandlerFunc) Handle(ctx context.Context, job domain.WorkerJob) error { return f(ctx, job) }

type Runner struct {
	store    *store.Store
	clock    clock.Clock
	owner    string
	poll     time.Duration
	lease    time.Duration
	handlers map[string]Handler
	logger   *slog.Logger
	wg       sync.WaitGroup
}

func New(store *store.Store, clock clock.Clock, owner string, poll, lease time.Duration, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{store: store, clock: clock, owner: owner, poll: poll, lease: lease,
		handlers: make(map[string]Handler), logger: logger}
}

func (r *Runner) Register(kind string, handler Handler) error {
	if kind == "" || handler == nil {
		return errors.New("worker kind and handler are required")
	}
	if _, exists := r.handlers[kind]; exists {
		return fmt.Errorf("handler %q already registered", kind)
	}
	r.handlers[kind] = handler
	return nil
}

func (r *Runner) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.poll)
	defer ticker.Stop()
	for {
		if err := r.runOnce(ctx); err != nil && !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, context.Canceled) {
			r.logger.ErrorContext(ctx, "worker cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) RunOnce(ctx context.Context) error { return r.runOnce(ctx) }

func (r *Runner) runOnce(ctx context.Context) error {
	job, err := r.store.ClaimJob(ctx, r.owner, r.clock.Now(), r.lease)
	if err != nil {
		return err
	}
	handler, ok := r.handlers[job.Kind]
	if !ok {
		return r.store.FailJob(ctx, job, fmt.Errorf("no handler registered for %q", job.Kind), r.clock.Now(), 0)
	}
	handleCtx, cancel := context.WithTimeout(ctx, r.lease)
	defer cancel()
	err = handler.Handle(handleCtx, job)
	if err == nil {
		return r.store.CompleteJob(ctx, job.ID, r.owner, r.clock.Now())
	}
	backoff := time.Duration(math.Pow(2, float64(job.Attempts-1))) * time.Second
	if backoff > time.Minute {
		backoff = time.Minute
	}
	return r.store.SettleFailedAttempt(handleCtx, job, fmt.Errorf("handle %s: %w", job.Kind, err), r.clock.Now(), backoff)
}
