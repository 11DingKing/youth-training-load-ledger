package planning

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

type ScreeningInput struct {
	Answers  map[string]any `json:"answers"`
	RiskFlag bool           `json:"risk_flag"`
}

type PrescriptionInput struct {
	AthleteID        int64                  `json:"athlete_id"`
	WeekStart        time.Time              `json:"week_start"`
	WeeklyLoadLimit  int                    `json:"weekly_load_limit"`
	MaxSessionLoad   int                    `json:"max_session_load"`
	MinRecoveryHours int                    `json:"min_recovery_hours"`
	StrengthDays     int                    `json:"strength_days"`
	Basis            string                 `json:"basis"`
	StrengthBlocks   []domain.StrengthBlock `json:"strength_blocks"`
}

func NewService(store *store.Store, clock clock.Clock) *Service {
	return &Service{store: store, clock: clock}
}

func (s *Service) SubmitScreening(ctx context.Context, actor domain.User, athleteID int64, input ScreeningInput) (domain.HealthScreening, error) {
	athlete, err := s.store.AthleteByID(ctx, athleteID)
	if err != nil {
		return domain.HealthScreening{}, err
	}
	if actor.ID != athlete.StudentUserID && actor.ID != athlete.GuardianUserID {
		return domain.HealthScreening{}, domain.ErrForbidden
	}
	if len(input.Answers) == 0 {
		return domain.HealthScreening{}, domain.FieldError{Field: "answers", Problem: "at least one answer is required"}
	}
	answers, err := json.Marshal(input.Answers)
	if err != nil {
		return domain.HealthScreening{}, domain.FieldError{Field: "answers", Problem: "cannot encode answers"}
	}
	decision := domain.ScreeningPending
	if input.RiskFlag {
		decision = domain.ScreeningReview
	}
	now := s.clock.Now()
	screening := domain.HealthScreening{AthleteID: athleteID, AnswersJSON: string(answers),
		Decision: decision, SubmittedAt: now}
	err = s.store.WithTx(ctx, func(tx *store.Tx) error {
		created, createErr := tx.CreateScreening(ctx, screening)
		if createErr != nil {
			return createErr
		}
		screening = created
		_, createErr = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "screening.submit",
			ObjectType: "screening", ObjectID: screening.ID, Outcome: "success", RequestID: audit.RequestID(ctx),
			Metadata: fmt.Sprintf(`{"athlete_id":%d,"risk_flag":%t}`, athleteID, input.RiskFlag), CreatedAt: now})
		return createErr
	})
	return screening, err
}

func (s *Service) ReviewScreening(ctx context.Context, actor domain.User, athleteID int64, clear bool, basis string) (domain.HealthScreening, error) {
	if actor.Role != domain.RoleAdvisor {
		return domain.HealthScreening{}, domain.ErrForbidden
	}
	now := s.clock.Now()
	var screening domain.HealthScreening
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		var err error
		screening, err = tx.LatestScreening(ctx, athleteID)
		if err != nil {
			return err
		}
		if err = screening.Review(actor, clear, basis, now); err != nil {
			return err
		}
		if err = tx.UpdateScreening(ctx, screening); err != nil {
			return err
		}
		outcome := "cleared"
		if !clear {
			outcome = "review_required"
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "screening.review",
			ObjectType: "screening", ObjectID: screening.ID, Outcome: "success", Basis: basis,
			RequestID: audit.RequestID(ctx), Metadata: fmt.Sprintf(`{"decision":%q}`, outcome), CreatedAt: now})
		return err
	})
	return screening, err
}

func (s *Service) RecordBaseline(ctx context.Context, actor domain.User, baseline domain.BaselineAssessment) (domain.BaselineAssessment, error) {
	if !actor.CanManageTraining() {
		return domain.BaselineAssessment{}, domain.ErrForbidden
	}
	athlete, err := s.store.AthleteByID(ctx, baseline.AthleteID)
	if err != nil {
		return domain.BaselineAssessment{}, err
	}
	if !athlete.Authorized(actor) {
		return domain.BaselineAssessment{}, domain.ErrForbidden
	}
	baseline.AssessorUserID = actor.ID
	if baseline.AssessedAt.IsZero() {
		baseline.AssessedAt = s.clock.Now()
	}
	if err = baseline.Validate(); err != nil {
		return domain.BaselineAssessment{}, err
	}
	err = s.store.WithTx(ctx, func(tx *store.Tx) error {
		created, createErr := tx.CreateBaseline(ctx, baseline)
		if createErr != nil {
			return createErr
		}
		baseline = created
		_, createErr = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "baseline.record",
			ObjectType: "baseline", ObjectID: baseline.ID, Outcome: "success", Basis: baseline.Conclusion,
			RequestID: audit.RequestID(ctx), Metadata: fmt.Sprintf(`{"athlete_id":%d,"sequence":%d}`, baseline.AthleteID, baseline.Sequence),
			CreatedAt: s.clock.Now()})
		return createErr
	})
	return baseline, err
}

func (s *Service) CreatePrescription(ctx context.Context, actor domain.User, input PrescriptionInput) (domain.Prescription, error) {
	if !actor.CanManageTraining() {
		return domain.Prescription{}, domain.ErrForbidden
	}
	athlete, err := s.store.AthleteByID(ctx, input.AthleteID)
	if err != nil {
		return domain.Prescription{}, err
	}
	if !athlete.Authorized(actor) {
		return domain.Prescription{}, domain.ErrForbidden
	}
	location, err := time.LoadLocation(athlete.Timezone)
	if err != nil {
		return domain.Prescription{}, fmt.Errorf("athlete timezone: %w", err)
	}
	now := s.clock.Now()
	prescription := domain.Prescription{
		AthleteID: input.AthleteID, AuthorUserID: actor.ID,
		WeekStart: domain.NormalizeWeekStart(input.WeekStart, location), Version: 1,
		Status: domain.PrescriptionDraft, WeeklyLoadLimit: input.WeeklyLoadLimit,
		MaxSessionLoad: input.MaxSessionLoad, MinRecoveryHours: input.MinRecoveryHours,
		StrengthDays: input.StrengthDays, Basis: input.Basis, CreatedAt: now,
	}
	if err = prescription.Validate(); err != nil {
		return domain.Prescription{}, err
	}
	seenDays := make(map[int]struct{})
	for index := range input.StrengthBlocks {
		block := &input.StrengthBlocks[index]
		if err = block.Validate(); err != nil {
			return domain.Prescription{}, err
		}
		seenDays[block.DayOffset] = struct{}{}
	}
	if len(seenDays) > prescription.StrengthDays {
		return domain.Prescription{}, domain.FieldError{Field: "strength_blocks", Problem: "uses more days than prescription allows"}
	}
	err = s.store.WithTx(ctx, func(tx *store.Tx) error {
		if latest, latestErr := tx.PublishedPrescription(ctx, input.AthleteID, prescription.WeekStart); latestErr == nil {
			prescription.Version = latest.Version + 1
		} else if !errors.Is(latestErr, domain.ErrNotFound) {
			return latestErr
		}
		created, createErr := tx.CreatePrescription(ctx, prescription)
		if createErr != nil {
			return createErr
		}
		prescription = created
		for _, block := range input.StrengthBlocks {
			block.PrescriptionID = prescription.ID
			block.CreatedAt = now
			if _, createErr = tx.CreateStrengthBlock(ctx, block); createErr != nil {
				return createErr
			}
		}
		_, createErr = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "prescription.create",
			ObjectType: "prescription", ObjectID: prescription.ID, Outcome: "success", Basis: prescription.Basis,
			RequestID: audit.RequestID(ctx), Metadata: fmt.Sprintf(`{"athlete_id":%d,"version":%d}`, prescription.AthleteID, prescription.Version),
			CreatedAt: now})
		return createErr
	})
	return prescription, err
}

func (s *Service) PublishPrescription(ctx context.Context, actor domain.User, prescriptionID, expectedVersion int64) (domain.Prescription, error) {
	if !actor.CanManageTraining() {
		return domain.Prescription{}, domain.ErrForbidden
	}
	now := s.clock.Now()
	var prescription domain.Prescription
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		var err error
		prescription, err = tx.PrescriptionByID(ctx, prescriptionID)
		if err != nil {
			return err
		}
		athlete, err := tx.AthleteByID(ctx, prescription.AthleteID)
		if err != nil {
			return err
		}
		if !athlete.Authorized(actor) || athlete.Status != domain.AthleteActive {
			return domain.ErrForbidden
		}
		if _, err = tx.LatestBaseline(ctx, athlete.ID); err != nil {
			return domain.ErrBaselineRequired
		}
		if prescription.Version != expectedVersion {
			return domain.ErrVersionConflict
		}
		if previous, previousErr := tx.PublishedPrescription(ctx, athlete.ID, prescription.WeekStart); previousErr == nil {
			previousVersion := previous.Version
			if err = previous.Supersede(now); err != nil {
				return err
			}
			previous.Version++
			if err = tx.UpdatePrescription(ctx, previous, previousVersion); err != nil {
				return err
			}
		} else if !errors.Is(previousErr, domain.ErrNotFound) {
			return previousErr
		}
		if err = prescription.Publish(now); err != nil {
			return err
		}
		prescription.Version++
		if err = tx.UpdatePrescription(ctx, prescription, expectedVersion); err != nil {
			return err
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "prescription.publish",
			ObjectType: "prescription", ObjectID: prescription.ID, Outcome: "success", Basis: prescription.Basis,
			RequestID: audit.RequestID(ctx), Metadata: fmt.Sprintf(`{"week_start":%q}`, prescription.WeekStart.Format(time.RFC3339)),
			CreatedAt: now})
		return err
	})
	return prescription, err
}

func (s *Service) RecordReassessment(ctx context.Context, actor domain.User, item domain.Reassessment) (domain.Reassessment, error) {
	if actor.Role != domain.RoleAdvisor {
		return domain.Reassessment{}, domain.ErrForbidden
	}
	item.AssessorUserID = actor.ID
	if item.AssessedAt.IsZero() {
		item.AssessedAt = s.clock.Now()
	}
	if err := item.Validate(); err != nil {
		return domain.Reassessment{}, err
	}
	result, err := s.store.CreateReassessment(ctx, item)
	if err != nil {
		return domain.Reassessment{}, err
	}
	err = s.store.WithTx(ctx, func(tx *store.Tx) error {
		baseline, err := tx.LatestBaseline(ctx, item.AthleteID)
		if err != nil || baseline.ID != item.BaselineID {
			return domain.ErrBaselineRequired
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "reassessment.record",
			ObjectType: "reassessment", ObjectID: result.ID, Outcome: "success", Basis: item.Basis,
			RequestID: audit.RequestID(ctx), Metadata: fmt.Sprintf(`{"athlete_id":%d}`, item.AthleteID), CreatedAt: s.clock.Now()})
		return err
	})
	return result, err
}
