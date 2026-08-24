package risk_test

import (
	"testing"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestFailedReassessmentDoesNotBecomeRecoveryEvidence(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := t.Context()
	if _, err := f.database.DB().ExecContext(ctx, `CREATE TRIGGER reject_reassessment_audit
		BEFORE INSERT ON audit_events WHEN NEW.action = 'reassessment.record'
		BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	item := domain.Reassessment{AthleteID: f.athlete.ID, BaselineID: f.baseline.ID,
		EnduranceScore: 61, StrengthScore: 60, MobilityScore: 72,
		Recommendation: "resume with reduced weekly load", Basis: "post-hold stage review"}
	if _, err := f.planning.RecordReassessment(ctx, f.advisor, item); err == nil {
		t.Fatal("reassessment succeeded while audit storage was unavailable")
	}
	var count int
	if err := f.database.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM reassessments").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed reassessment became recovery evidence: count=%d", count)
	}
	if _, err := f.database.DB().ExecContext(ctx, "DROP TRIGGER reject_reassessment_audit"); err != nil {
		t.Fatal(err)
	}
	created, err := f.planning.RecordReassessment(ctx, f.advisor, item)
	if err != nil || created.ID == 0 {
		t.Fatalf("valid reassessment failed: item=%+v err=%v", created, err)
	}
}
