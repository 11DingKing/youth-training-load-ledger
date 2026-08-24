package risk_test

import (
	"testing"

	activityservice "github.com/11DingKing/youth-training-load-ledger/internal/activity"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestActivityAuditFailureRollsBackLoadHistory(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := t.Context()
	if _, err := f.database.DB().ExecContext(ctx, `CREATE TRIGGER reject_activity_audit
		BEFORE INSERT ON audit_events WHEN NEW.action = 'activity.record'
		BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	input := activityservice.RecordInput{AthleteID: f.athlete.ID, PrescriptionID: f.prescription.ID,
		IdempotencyKey: "late-audit-1", OccurredAt: f.now.Now(), DurationMinutes: 20,
		PerceivedEffort: 4, Source: domain.ActivityStudent}
	if _, err := f.activities.Record(ctx, f.student, input); err == nil {
		t.Fatal("activity succeeded while audit storage was unavailable")
	}
	var activities, snapshots int
	if err := f.database.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_logs").Scan(&activities); err != nil {
		t.Fatal(err)
	}
	if err := f.database.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM load_snapshots").Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if activities != 0 || snapshots != 0 {
		t.Fatalf("failed activity changed load history: activities=%d snapshots=%d", activities, snapshots)
	}
	if _, err := f.database.DB().ExecContext(ctx, "DROP TRIGGER reject_activity_audit"); err != nil {
		t.Fatal(err)
	}
	result, err := f.activities.Record(ctx, f.student, input)
	if err != nil || result.Activity.ID == 0 || result.Snapshot.ID == 0 {
		t.Fatalf("retry did not record activity atomically: result=%+v err=%v", result, err)
	}
}
