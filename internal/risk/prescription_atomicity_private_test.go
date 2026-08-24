package risk_test

import (
	"errors"
	"testing"

	"github.com/11DingKing/youth-training-load-ledger/internal/audit"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	planningservice "github.com/11DingKing/youth-training-load-ledger/internal/planning"
)

func TestFailedPrescriptionPublishPreservesCurrentVersion(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := audit.WithRequestID(t.Context(), "prescription-atomicity")

	candidate, err := f.planning.CreatePrescription(ctx, f.coach, planningservice.PrescriptionInput{
		AthleteID: f.athlete.ID, WeekStart: f.now.Now(), WeeklyLoadLimit: 360,
		MaxSessionLoad: 180, MinRecoveryHours: 12, StrengthDays: 1,
		Basis: "stage review lowers load after first training week",
		StrengthBlocks: []domain.StrengthBlock{
			{DayOffset: 2, MuscleGroup: "core", Sets: 2, Repetitions: 10, IntensityRPE: 5},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	staleVersion := candidate.Version - 1
	if _, err = f.planning.PublishPrescription(ctx, f.coach, candidate.ID, staleVersion); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale publish error = %v, want version conflict", err)
	}

	current, err := f.database.PrescriptionByID(ctx, f.prescription.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedCandidate, err := f.database.PrescriptionByID(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.PrescriptionPublished || failedCandidate.Status != domain.PrescriptionDraft {
		t.Fatalf("failed publish changed versions: current=%s candidate=%s", current.Status, failedCandidate.Status)
	}

	published, err := f.planning.PublishPrescription(ctx, f.coach, candidate.ID, candidate.Version)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := f.database.PrescriptionByID(ctx, f.prescription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != domain.PrescriptionPublished || previous.Status != domain.PrescriptionSuperseded {
		t.Fatalf("successful publish did not switch versions: previous=%s current=%s", previous.Status, published.Status)
	}
}
