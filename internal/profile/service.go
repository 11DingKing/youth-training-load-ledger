package profile

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

type CreateAthleteInput struct {
	StudentUserID  int64     `json:"student_user_id"`
	GuardianUserID int64     `json:"guardian_user_id"`
	CoachUserID    *int64    `json:"coach_user_id,omitempty"`
	AdvisorUserID  *int64    `json:"advisor_user_id,omitempty"`
	BirthDate      time.Time `json:"birth_date"`
	Timezone       string    `json:"timezone"`
	TermsVersion   string    `json:"terms_version"`
}

func NewService(store *store.Store, clock clock.Clock) *Service {
	return &Service{store: store, clock: clock}
}

func (s *Service) CreateAthlete(ctx context.Context, actor domain.User, input CreateAthleteInput) (domain.Athlete, error) {
	if actor.Role != domain.RoleAdvisor {
		return domain.Athlete{}, domain.ErrForbidden
	}
	if input.TermsVersion == "" {
		return domain.Athlete{}, domain.FieldError{Field: "terms_version", Problem: "is required"}
	}
	now := s.clock.Now()
	athlete := domain.Athlete{
		StudentUserID: input.StudentUserID, GuardianUserID: input.GuardianUserID,
		CoachUserID: input.CoachUserID, AdvisorUserID: input.AdvisorUserID,
		BirthDate: input.BirthDate, Timezone: input.Timezone, Status: domain.AthleteDraft,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := athlete.Validate(now); err != nil {
		return domain.Athlete{}, err
	}
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		student, err := tx.UserByID(ctx, input.StudentUserID)
		if err != nil {
			return fmt.Errorf("student relationship: %w", err)
		}
		guardian, err := tx.UserByID(ctx, input.GuardianUserID)
		if err != nil {
			return fmt.Errorf("guardian relationship: %w", err)
		}
		if student.Role != domain.RoleStudent || guardian.Role != domain.RoleGuardian {
			return domain.FieldError{Field: "relationships", Problem: "user roles do not match athlete relationships"}
		}
		created, err := tx.CreateAthlete(ctx, athlete)
		if err != nil {
			return err
		}
		athlete = created
		consent := domain.GuardianConsent{AthleteID: athlete.ID, GuardianID: guardian.ID,
			Status: domain.ConsentPending, TermsVersion: input.TermsVersion, CreatedAt: now}
		if _, err = tx.CreateConsent(ctx, consent); err != nil {
			return err
		}
		from := athlete.Status
		if err = athlete.Transition(domain.AthleteAwaitingConsent, "guardian consent requested", now); err != nil {
			return err
		}
		if err = tx.UpdateAthlete(ctx, athlete, 1); err != nil {
			return err
		}
		if err = tx.InsertTransition(ctx, domain.StatusTransition{AthleteID: athlete.ID, ActorID: actor.ID,
			FromStatus: string(from), ToStatus: string(athlete.Status), Reason: "guardian consent requested",
			RequestID: audit.RequestID(ctx), OccurredAt: now}); err != nil {
			return err
		}
		metadata, _ := json.Marshal(map[string]any{"student_id": student.ID, "guardian_id": guardian.ID})
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "athlete.create",
			ObjectType: "athlete", ObjectID: athlete.ID, Outcome: "success", RequestID: audit.RequestID(ctx),
			Metadata: string(metadata), CreatedAt: now})
		return err
	})
	return athlete, err
}

func (s *Service) GrantConsent(ctx context.Context, actor domain.User, athleteID int64, expiresAt time.Time) (domain.Athlete, error) {
	if actor.Role != domain.RoleGuardian {
		return domain.Athlete{}, domain.ErrForbidden
	}
	now := s.clock.Now()
	var athlete domain.Athlete
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		var err error
		athlete, err = tx.AthleteByID(ctx, athleteID)
		if err != nil {
			return err
		}
		if athlete.GuardianUserID != actor.ID {
			return domain.ErrForbidden
		}
		consent, err := tx.CurrentConsent(ctx, athleteID)
		if err != nil {
			return err
		}
		if consent.GuardianID != actor.ID {
			return domain.ErrForbidden
		}
		if err = consent.Grant(now, expiresAt); err != nil {
			return err
		}
		if err = tx.UpdateConsent(ctx, consent); err != nil {
			return err
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "consent.grant",
			ObjectType: "athlete", ObjectID: athlete.ID, Outcome: "success", Basis: consent.TermsVersion,
			RequestID: audit.RequestID(ctx), Metadata: `{}`, CreatedAt: now})
		return err
	})
	return athlete, err
}

func (s *Service) Activate(ctx context.Context, actor domain.User, athleteID int64) (domain.Athlete, error) {
	if !actor.CanResolveRisk() {
		return domain.Athlete{}, domain.ErrForbidden
	}
	now := s.clock.Now()
	var athlete domain.Athlete
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		var err error
		athlete, err = tx.AthleteByID(ctx, athleteID)
		if err != nil {
			return err
		}
		consent, err := tx.CurrentConsent(ctx, athleteID)
		if err != nil || !consent.ValidAt(now) {
			return domain.ErrConsentRequired
		}
		screening, err := tx.LatestScreening(ctx, athleteID)
		if err != nil || screening.Decision != domain.ScreeningCleared {
			return fmt.Errorf("%w: screening is not cleared", domain.ErrInvalidState)
		}
		if _, err = tx.LatestBaseline(ctx, athleteID); err != nil {
			return domain.ErrBaselineRequired
		}
		before := athlete.Status
		expected := athlete.Version
		if err = athlete.Transition(domain.AthleteActive, "eligibility completed", now); err != nil {
			return err
		}
		if err = tx.UpdateAthlete(ctx, athlete, expected); err != nil {
			return err
		}
		if err = tx.InsertTransition(ctx, domain.StatusTransition{AthleteID: athlete.ID, ActorID: actor.ID,
			FromStatus: string(before), ToStatus: string(athlete.Status), Reason: "eligibility completed",
			RequestID: audit.RequestID(ctx), OccurredAt: now}); err != nil {
			return err
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "athlete.activate",
			ObjectType: "athlete", ObjectID: athlete.ID, Outcome: "success", Basis: "consent, screening and baseline verified",
			RequestID: audit.RequestID(ctx), Metadata: `{}`, CreatedAt: now})
		return err
	})
	return athlete, err
}

func (s *Service) WithdrawConsent(ctx context.Context, actor domain.User, athleteID int64, reason string) (domain.Athlete, error) {
	if actor.Role != domain.RoleGuardian || reason == "" {
		return domain.Athlete{}, domain.ErrForbidden
	}
	now := s.clock.Now()
	var athlete domain.Athlete
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		var err error
		athlete, err = tx.AthleteByID(ctx, athleteID)
		if err != nil {
			return err
		}
		if athlete.GuardianUserID != actor.ID {
			return domain.ErrForbidden
		}
		consent, err := tx.CurrentConsent(ctx, athleteID)
		if err != nil {
			return err
		}
		if err = consent.Withdraw(now); err != nil {
			return err
		}
		if err = tx.UpdateConsent(ctx, consent); err != nil {
			return err
		}
		if athlete.Status == domain.AthleteActive {
			before, expected := athlete.Status, athlete.Version
			if err = athlete.Transition(domain.AthletePaused, "guardian consent withdrawn: "+reason, now); err != nil {
				return err
			}
			if err = tx.UpdateAthlete(ctx, athlete, expected); err != nil {
				return err
			}
			if err = tx.InsertTransition(ctx, domain.StatusTransition{AthleteID: athlete.ID, ActorID: actor.ID,
				FromStatus: string(before), ToStatus: string(athlete.Status), Reason: athlete.PausedReason,
				RequestID: audit.RequestID(ctx), OccurredAt: now}); err != nil {
				return err
			}
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "consent.withdraw",
			ObjectType: "athlete", ObjectID: athlete.ID, Outcome: "success", Basis: reason,
			RequestID: audit.RequestID(ctx), Metadata: `{}`, CreatedAt: now})
		return err
	})
	return athlete, err
}

func (s *Service) GetAuthorized(ctx context.Context, actor domain.User, athleteID int64) (domain.Athlete, error) {
	athlete, err := s.store.AthleteByID(ctx, athleteID)
	if err != nil {
		return domain.Athlete{}, err
	}
	if !athlete.Authorized(actor) {
		return domain.Athlete{}, domain.ErrForbidden
	}
	return athlete, nil
}

func IsEligibilityError(err error) bool {
	return errors.Is(err, domain.ErrConsentRequired) || errors.Is(err, domain.ErrBaselineRequired) || errors.Is(err, domain.ErrInvalidState)
}
