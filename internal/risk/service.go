package risk

import (
	"context"
	"encoding/json"
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

func NewService(store *store.Store, clock clock.Clock) *Service {
	return &Service{store: store, clock: clock}
}

func (s *Service) SubmitFatigue(ctx context.Context, actor domain.User, report domain.FatigueReport) (domain.FatigueReport, *domain.RiskCase, error) {
	athlete, err := s.store.AthleteByID(ctx, report.AthleteID)
	if err != nil {
		return domain.FatigueReport{}, nil, err
	}
	if actor.ID != athlete.StudentUserID && actor.ID != athlete.GuardianUserID {
		return domain.FatigueReport{}, nil, domain.ErrForbidden
	}
	report.ReporterUserID = actor.ID
	report.ReportedFor = report.ReportedFor.UTC()
	report.CreatedAt = s.clock.Now()
	if err = report.Validate(); err != nil {
		return domain.FatigueReport{}, nil, err
	}
	var opened *domain.RiskCase
	err = s.store.WithTx(ctx, func(tx *store.Tx) error {
		created, createErr := tx.CreateFatigue(ctx, report)
		if createErr != nil {
			return createErr
		}
		report = created
		if severity, risky := report.Severity(); risky {
			riskCase := domain.RiskCase{AthleteID: athlete.ID, TriggerType: "fatigue_report",
				TriggerReferenceID: report.ID, Severity: severity, Status: domain.RiskOpen,
				Basis:    fmt.Sprintf("fatigue=%d soreness=%d sleep=%.1f", report.FatigueScore, report.SorenessScore, report.SleepHours),
				OpenedAt: report.CreatedAt, Version: 1}
			riskCase, createErr = tx.CreateRisk(ctx, riskCase)
			if createErr != nil {
				return createErr
			}
			opened = &riskCase
			if athlete.Status == domain.AthleteActive {
				before, version := athlete.Status, athlete.Version
				if createErr = athlete.Transition(domain.AthletePaused, "fatigue risk hold", report.CreatedAt); createErr != nil {
					return createErr
				}
				if createErr = tx.UpdateAthlete(ctx, athlete, version); createErr != nil {
					return createErr
				}
				if createErr = tx.InsertTransition(ctx, domain.StatusTransition{AthleteID: athlete.ID, ActorID: actor.ID,
					FromStatus: string(before), ToStatus: string(athlete.Status), Reason: athlete.PausedReason,
					RequestID: audit.RequestID(ctx), OccurredAt: report.CreatedAt}); createErr != nil {
					return createErr
				}
			}
			payload, _ := json.Marshal(map[string]any{"risk_case_id": riskCase.ID, "athlete_id": athlete.ID})
			if _, createErr = tx.EnqueueJob(ctx, domain.WorkerJob{Kind: "risk_notification", Payload: string(payload),
				Status: domain.JobPending, MaxAttempts: 5, AvailableAt: report.CreatedAt,
				CreatedAt: report.CreatedAt, UpdatedAt: report.CreatedAt}); createErr != nil {
				return createErr
			}
		}
		_, createErr = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "fatigue.submit",
			ObjectType: "fatigue_report", ObjectID: report.ID, Outcome: "success", RequestID: audit.RequestID(ctx),
			Metadata: fmt.Sprintf(`{"athlete_id":%d}`, report.AthleteID), CreatedAt: report.CreatedAt})
		return createErr
	})
	return report, opened, err
}

func (s *Service) Acknowledge(ctx context.Context, actor domain.User, riskID, expectedVersion int64) (domain.RiskCase, error) {
	now := s.clock.Now()
	riskCase, err := s.store.RiskByID(ctx, riskID)
	if err != nil {
		return domain.RiskCase{}, err
	}
	if riskCase.Version != expectedVersion {
		return domain.RiskCase{}, domain.ErrVersionConflict
	}
	if err = riskCase.Acknowledge(actor, now); err != nil {
		return domain.RiskCase{}, err
	}
	if err = s.store.UpdateRisk(ctx, riskCase, expectedVersion); err != nil {
		return domain.RiskCase{}, err
	}
	err = s.store.WithTx(ctx, func(tx *store.Tx) error {
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "risk.acknowledge",
			ObjectType: "risk_case", ObjectID: riskCase.ID, Outcome: "success", Basis: riskCase.Basis,
			RequestID: audit.RequestID(ctx), Metadata: `{}`, CreatedAt: now})
		return err
	})
	return riskCase, err
}

func (s *Service) Resolve(ctx context.Context, actor domain.User, riskID, expectedVersion int64, resolution string) (domain.RiskCase, error) {
	now := s.clock.Now()
	var riskCase domain.RiskCase
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		var err error
		riskCase, err = tx.RiskByID(ctx, riskID)
		if err != nil {
			return err
		}
		if riskCase.Version != expectedVersion {
			return domain.ErrVersionConflict
		}
		if err = riskCase.Resolve(actor, resolution, now); err != nil {
			return err
		}
		if err = tx.UpdateRisk(ctx, riskCase, expectedVersion); err != nil {
			return err
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "risk.resolve",
			ObjectType: "risk_case", ObjectID: riskCase.ID, Outcome: "success", Basis: resolution,
			RequestID: audit.RequestID(ctx), Metadata: `{}`, CreatedAt: now})
		return err
	})
	return riskCase, err
}

func (s *Service) ResumeAthlete(ctx context.Context, actor domain.User, athleteID, expectedVersion int64, basis string) (domain.Athlete, error) {
	if actor.Role != domain.RoleAdvisor || basis == "" {
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
		if athlete.Version != expectedVersion {
			return domain.ErrVersionConflict
		}
		open, err := tx.CountOpenRisks(ctx, athleteID)
		if err != nil {
			return err
		}
		if open != 0 {
			return domain.ErrRiskOpen
		}
		latest, err := tx.LatestReassessment(ctx, athleteID)
		if err != nil {
			return fmt.Errorf("%w: reassessment required", domain.ErrInvalidState)
		}
		if now.Sub(latest.AssessedAt) > 14*24*time.Hour {
			return fmt.Errorf("%w: reassessment is stale", domain.ErrInvalidState)
		}
		before := athlete.Status
		if err = athlete.Transition(domain.AthleteActive, basis, now); err != nil {
			return err
		}
		if err = tx.UpdateAthlete(ctx, athlete, expectedVersion); err != nil {
			return err
		}
		if err = tx.InsertTransition(ctx, domain.StatusTransition{AthleteID: athlete.ID, ActorID: actor.ID,
			FromStatus: string(before), ToStatus: string(athlete.Status), Reason: basis,
			RequestID: audit.RequestID(ctx), OccurredAt: now}); err != nil {
			return err
		}
		_, err = tx.InsertAudit(ctx, domain.AuditEvent{ActorID: actor.ID, Action: "athlete.resume",
			ObjectType: "athlete", ObjectID: athlete.ID, Outcome: "success", Basis: basis,
			RequestID: audit.RequestID(ctx), Metadata: fmt.Sprintf(`{"reassessment_id":%d}`, latest.ID), CreatedAt: now})
		return err
	})
	return athlete, err
}
