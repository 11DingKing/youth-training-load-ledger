package risk_test

import (
	"testing"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestRiskAcknowledgeAuditFailurePreservesOpenCase(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := t.Context()
	_, opened, err := f.risks.SubmitFatigue(ctx, f.student, domain.FatigueReport{
		AthleteID: f.athlete.ID, ReportedFor: f.now.Now(), FatigueScore: 9,
		SorenessScore: 8, SleepHours: 3, Notes: "recovery concern"})
	if err != nil || opened == nil {
		t.Fatalf("open risk setup failed: risk=%+v err=%v", opened, err)
	}
	if _, err = f.database.DB().ExecContext(ctx, `CREATE TRIGGER reject_risk_ack_audit
		BEFORE INSERT ON audit_events WHEN NEW.action = 'risk.acknowledge'
		BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.risks.Acknowledge(ctx, f.advisor, opened.ID, opened.Version); err == nil {
		t.Fatal("risk acknowledgement succeeded while audit storage was unavailable")
	}
	stored, err := f.database.RiskByID(ctx, opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.RiskOpen || stored.AssignedAdvisorID != nil || stored.Version != opened.Version {
		t.Fatalf("failed acknowledgement changed risk: status=%s advisor=%v version=%d",
			stored.Status, stored.AssignedAdvisorID, stored.Version)
	}
	if _, err = f.database.DB().ExecContext(ctx, "DROP TRIGGER reject_risk_ack_audit"); err != nil {
		t.Fatal(err)
	}
	acknowledged, err := f.risks.Acknowledge(ctx, f.advisor, opened.ID, opened.Version)
	if err != nil || acknowledged.Status != domain.RiskAcknowledged {
		t.Fatalf("valid acknowledgement failed: risk=%+v err=%v", acknowledged, err)
	}
}
