package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/audit"
	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
)

type Service struct {
	store *store.Store
	clock clock.Clock
}

type RecordInput struct {
	AthleteID       int64                 `json:"athlete_id"`
	PrescriptionID  int64                 `json:"prescription_id"`
	IdempotencyKey  string                `json:"idempotency_key"`
	OccurredAt      time.Time             `json:"occurred_at"`
	DurationMinutes int                   `json:"duration_minutes"`
	PerceivedEffort int                   `json:"perceived_effort"`
	Source          domain.ActivitySource `json:"source"`
	SupersedesID    *int64                `json:"supersedes_id,omitempty"`
}

type RecordResult struct {
	Activity domain.ActivityLog  `json:"activity"`
	Snapshot domain.LoadSnapshot `json:"load_snapshot"`
	Risk     *domain.RiskCase    `json:"risk_case,omitempty"`
	Replayed bool                `json:"replayed"`
}

func NewService(store *store.Store, clock clock.Clock) *Service {
	return &Service{store: store, clock: clock}
}

func (s *Service) Record(ctx context.Context, actor domain.User, input RecordInput) (RecordResult, error) {
	if actor.Role != domain.RoleStudent && actor.Role != domain.RoleCoach {
		return RecordResult{}, domain.ErrForbidden
	}
	now := s.clock.Now()
	activity := domain.ActivityLog{
		AthleteID: input.AthleteID, PrescriptionID: input.PrescriptionID, RecorderUserID: actor.ID,
		IdempotencyKey: input.IdempotencyKey, OccurredAt: input.OccurredAt.UTC(), RecordedAt: now,
		DurationMinutes: input.DurationMinutes, PerceivedEffort: input.PerceivedEffort,
		Source: input.Source, SupersedesID: input.SupersedesID, CreatedAt: now,
	}
	if activity.Source == "" {
		if actor.Role == domain.RoleCoach {
			activity.Source = domain.ActivityCoach
		} else {
			activity.Source = domain.ActivityStudent
		}
	}
	if err := activity.Validate(now); err != nil {
		return RecordResult{}, err
	}
	result := RecordResult{}
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		if existing, findErr := tx.ActivityByIdempotency(ctx, input.AthleteID, input.IdempotencyKey); findErr == nil {
			if existing.PrescriptionID != input.PrescriptionID || existing.OccurredAt.UTC() != input.OccurredAt.UTC() ||
				existing.DurationMinutes != input.DurationMinutes || existing.PerceivedEffort != input.PerceivedEffort {
				return fmt.Errorf("%w: idempotency key used with different request", domain.ErrConflict)
			}
			result.Activity = existing
			result.Replayed = true
			return nil
		} else if !errors.Is(findErr, domain.ErrNotFound) {
			return findErr
		}
		athlete, err := tx.AthleteByID(ctx, input.AthleteID)
		if err != nil {
			return err
		}
		if !athlete.Authorized(actor) {
			return domain.ErrForbidden
		}
		if athlete.Status == domain.AthletePaused {
			return domain.ErrTrainingPaused
		}
		if athlete.Status != domain.AthleteActive {
			return fmt.Errorf("%w: athlete is not active", domain.ErrInvalidState)
		}
		prescription, err := tx.PrescriptionByID(ctx, input.PrescriptionID)
		if err != nil {
			return err
		}
		if prescription.AthleteID != athlete.ID || prescription.Status != domain.PrescriptionPublished {
			return fmt.Errorf("%w: prescription is not active for athlete", domain.ErrInvalidState)
		}
		if activity.LoadUnits > prescription.MaxSessionLoad {
			return domain.ErrLoadExceeded
		}
		windowStart := activity.OccurredAt.Add(-7 * 24 * time.Hour)
		prior, err := tx.ActivitiesInWindow(ctx, athlete.ID, windowStart, activity.OccurredAt)
		if err != nil {
			return err
		}
		for _, previous := range prior {
			if previous.Source == domain.ActivityCorrection || previous.SupersedesID != nil {
				continue
			}
			recovery := activity.OccurredAt.Sub(previous.OccurredAt)
			if recovery >= 0 && recovery < time.Duration(prescription.MinRecoveryHours)*time.Hour && activity.LoadUnits > prescription.MaxSessionLoad/2 {
				return fmt.Errorf("%w: recovery interval is %s", domain.ErrLoadExceeded, recovery)
			}
		}
		created, err := tx.CreateActivity(ctx, activity)
		if err != nil {
			return err
		}
		activity = created
		snapshot, err := domain.BuildLoadSnapshot(activity, prior, prescription.WeeklyLoadLimit, now)
		if err != nil {
			return err
		}
		snapshot, err = tx.CreateLoadSnapshot(ctx, snapshot)
		if err != nil {
			return err
		}
		result.Activity = activity
		result.Snapshot = snapshot
		if snapshot.RiskTriggered {
			severity := domain.RiskHigh
			if snapshot.SevenDayLoad > prescription.WeeklyLoadLimit*3/2 {
				severity = domain.RiskCritical
			}
			risk := domain.RiskCase{AthleteID: athlete.ID, TriggerType: "load_snapshot",
				TriggerReferenceID: snapshot.ID, Severity: severity, Status: domain.RiskOpen,
				Basis:    fmt.Sprintf("seven-day load %d exceeds threshold %d", snapshot.SevenDayLoad, snapshot.Threshold),
				OpenedAt: now, Version: 1}
			risk, err = tx.CreateRisk(ctx, risk)
			if err != nil {
				return err
			}
			result.Risk = &risk
			before, version := athlete.Status, athlete.Version
			if err = athlete.Transition(domain.AthletePaused, "automatic load risk hold", now); err != nil {
				return err
			}
			if err = tx.UpdateAthlete(ctx, athlete, version); err != nil {
				return err
			}
			if err = tx.InsertTransition(ctx, domain.StatusTransition{AthleteID: athlete.ID, ActorID: actor.ID,
				FromStatus: string(before), ToStatus: string(athlete.Status), Reason: athlete.PausedReason,
				RequestID: audit.RequestID(ctx), OccurredAt: now}); err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{"risk_case_id": risk.ID, "athlete_id": athlete.ID})
			_, err = tx.EnqueueJob(ctx, domain.WorkerJob{Kind: "risk_notification", Payload: string(payload),
				Status: domain.JobPending, MaxAttempts: 5, AvailableAt: now, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "activity.record",
			ObjectType: "activity", ObjectID: activity.ID, Outcome: "success", RequestID: audit.RequestID(ctx),
			Metadata:  fmt.Sprintf(`{"athlete_id":%d,"load":%d,"late":%t}`, input.AthleteID, activity.LoadUnits, activity.LateEntry),
			CreatedAt: now})
		return err
	})
	return result, err
}
