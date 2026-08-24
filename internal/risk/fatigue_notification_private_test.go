package risk_test

import (
	"testing"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestNotificationFailureRollsBackFatigueRiskAndPause(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := t.Context()
	if _, err := f.database.DB().ExecContext(ctx, `CREATE TRIGGER reject_risk_notification
		BEFORE INSERT ON worker_jobs WHEN NEW.kind = 'risk_notification'
		BEGIN SELECT RAISE(ABORT, 'notification queue unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	report := domain.FatigueReport{AthleteID: f.athlete.ID, ReportedFor: f.now.Now(),
		FatigueScore: 9, SorenessScore: 8, SleepHours: 2.5, Notes: "unusual exhaustion"}
	if _, _, err := f.risks.SubmitFatigue(ctx, f.student, report); err == nil {
		t.Fatal("fatigue submission succeeded while notification queue was unavailable")
	}
	var reports, risks, jobs int
	for query, target := range map[string]*int{
		"SELECT COUNT(*) FROM fatigue_reports": &reports,
		"SELECT COUNT(*) FROM risk_cases":      &risks,
		"SELECT COUNT(*) FROM worker_jobs":     &jobs,
	} {
		if err := f.database.DB().QueryRowContext(ctx, query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	athlete, err := f.database.AthleteByID(ctx, f.athlete.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reports != 0 || risks != 0 || jobs != 0 || athlete.Status != domain.AthleteActive {
		t.Fatalf("failed fatigue submission leaked state: reports=%d risks=%d jobs=%d athlete=%s",
			reports, risks, jobs, athlete.Status)
	}
	if _, err = f.database.DB().ExecContext(ctx, `DROP TRIGGER reject_risk_notification`); err != nil {
		t.Fatal(err)
	}
	if _, opened, err := f.risks.SubmitFatigue(ctx, f.student, report); err != nil || opened == nil {
		t.Fatalf("retry did not open risk: risk=%+v err=%v", opened, err)
	}
	athlete, err = f.database.AthleteByID(ctx, f.athlete.ID)
	if err != nil || athlete.Status != domain.AthletePaused {
		t.Fatalf("retry did not pause athlete: athlete=%+v err=%v", athlete, err)
	}
}
