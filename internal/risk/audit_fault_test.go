package risk_test

import (
	"testing"
	"time"

	activityservice "github.com/11DingKing/youth-training-load-ledger/internal/activity"
	"github.com/11DingKing/youth-training-load-ledger/internal/audit"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

// TestActivityRecordRollsBackOnAuditFailure guards against the irreversible
// load pollution described when the audit sink fails: if the audit write is
// not part of the same transaction as the activity and load snapshot, a failed
// record attempt would still leave the activity and snapshot committed, and a
// retry with the same idempotency key would be wrongly treated as a replay.
// Here an audit-storage fault is simulated with a BEFORE INSERT trigger, then
// lifted so the original idempotency key can be retried as a fresh create.
func TestActivityRecordRollsBackOnAuditFailure(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := audit.WithRequestID(t.Context(), "audit-fault")

	if _, err := f.database.DB().ExecContext(ctx,
		`CREATE TRIGGER audit_sink_unavailable BEFORE INSERT ON audit_events `+
			`BEGIN SELECT RAISE(ABORT, 'audit sink unavailable'); END;`); err != nil {
		t.Fatalf("install audit fault trigger: %v", err)
	}

	input := activityservice.RecordInput{AthleteID: f.athlete.ID, PrescriptionID: f.prescription.ID,
		IdempotencyKey: "late-entry-1", OccurredAt: f.now.Now().Add(-25 * time.Hour),
		DurationMinutes: 20, PerceivedEffort: 4, Source: domain.ActivityStudent}

	if _, err := f.activities.Record(ctx, f.student, input); err == nil {
		t.Fatal("record with audit sink down unexpectedly succeeded")
	}

	var activities, snapshots int
	if err := f.database.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_logs`).Scan(&activities); err != nil {
		t.Fatal(err)
	}
	if err := f.database.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM load_snapshots`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if activities != 0 || snapshots != 0 {
		t.Fatalf("failed record left partial state: activities=%d snapshots=%d", activities, snapshots)
	}

	if _, err := f.database.DB().ExecContext(ctx, `DROP TRIGGER audit_sink_unavailable;`); err != nil {
		t.Fatalf("lift audit fault trigger: %v", err)
	}

	retry, err := f.activities.Record(ctx, f.student, input)
	if err != nil {
		t.Fatalf("retry with same idempotency key: %v", err)
	}
	if retry.Replayed {
		t.Fatalf("retry treated as replay despite rolled-back first attempt: %+v", retry)
	}
	if retry.Activity.ID == 0 || retry.Snapshot.ID == 0 {
		t.Fatalf("retry did not create activity and snapshot: %+v", retry)
	}

	var retryActivities, retrySnapshots int
	if err := f.database.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_logs`).Scan(&retryActivities); err != nil {
		t.Fatal(err)
	}
	if err := f.database.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM load_snapshots`).Scan(&retrySnapshots); err != nil {
		t.Fatal(err)
	}
	if retryActivities != 1 || retrySnapshots != 1 {
		t.Fatalf("retry counts = activities:%d snapshots:%d", retryActivities, retrySnapshots)
	}
}
