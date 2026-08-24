package risk_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	activityservice "github.com/11DingKing/youth-training-load-ledger/internal/activity"
	"github.com/11DingKing/youth-training-load-ledger/internal/audit"
	"github.com/11DingKing/youth-training-load-ledger/internal/auth"
	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	planningservice "github.com/11DingKing/youth-training-load-ledger/internal/planning"
	profileservice "github.com/11DingKing/youth-training-load-ledger/internal/profile"
	riskservice "github.com/11DingKing/youth-training-load-ledger/internal/risk"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
)

type workflowFixture struct {
	t            *testing.T
	now          *clock.Fixed
	database     *store.Store
	auth         *auth.Service
	profiles     *profileservice.Service
	planning     *planningservice.Service
	activities   *activityservice.Service
	risks        *riskservice.Service
	student      domain.User
	guardian     domain.User
	coach        domain.User
	advisor      domain.User
	athlete      domain.Athlete
	baseline     domain.BaselineAssessment
	prescription domain.Prescription
}

func newWorkflowFixture(t *testing.T) *workflowFixture {
	t.Helper()
	now := clock.NewFixed(time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	fixture := &workflowFixture{
		t: t, now: now, database: database,
		auth:       auth.NewService(database, now, 12*time.Hour),
		profiles:   profileservice.NewService(database, now),
		planning:   planningservice.NewService(database, now),
		activities: activityservice.NewService(database, now),
		risks:      riskservice.NewService(database, now),
	}
	fixture.student = fixture.register("student@workflow.test", domain.RoleStudent)
	fixture.guardian = fixture.register("guardian@workflow.test", domain.RoleGuardian)
	fixture.coach = fixture.register("coach@workflow.test", domain.RoleCoach)
	fixture.advisor = fixture.register("advisor@workflow.test", domain.RoleAdvisor)
	return fixture
}

func (f *workflowFixture) register(email string, role domain.Role) domain.User {
	f.t.Helper()
	user, err := f.auth.Register(f.t.Context(), email, email, "strong-password", role)
	if err != nil {
		f.t.Fatal(err)
	}
	return user
}

func (f *workflowFixture) prepareActiveAthlete() {
	f.t.Helper()
	ctx := audit.WithRequestID(f.t.Context(), "workflow-setup")
	athlete, err := f.profiles.CreateAthlete(ctx, f.advisor, profileservice.CreateAthleteInput{
		StudentUserID: f.student.ID, GuardianUserID: f.guardian.ID, CoachUserID: &f.coach.ID,
		AdvisorUserID: &f.advisor.ID, BirthDate: f.now.Now().AddDate(-14, 0, 0),
		Timezone: "UTC", TermsVersion: "summer-2026-v1",
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if athlete.Status != domain.AthleteAwaitingConsent || athlete.Version != 2 {
		f.t.Fatalf("created athlete = %+v", athlete)
	}
	f.athlete = athlete
	if _, err = f.profiles.GrantConsent(ctx, f.guardian, athlete.ID, f.now.Now().Add(60*24*time.Hour)); err != nil {
		f.t.Fatal(err)
	}
	if _, err = f.planning.SubmitScreening(ctx, f.student, athlete.ID, planningservice.ScreeningInput{
		Answers: map[string]any{"chest_pain": false, "medication": false}, RiskFlag: false,
	}); err != nil {
		f.t.Fatal(err)
	}
	if _, err = f.planning.ReviewScreening(ctx, f.advisor, athlete.ID, true, "answers and history reviewed"); err != nil {
		f.t.Fatal(err)
	}
	f.baseline, err = f.planning.RecordBaseline(ctx, f.coach, domain.BaselineAssessment{
		AthleteID: athlete.ID, EnduranceScore: 58, StrengthScore: 55, MobilityScore: 70,
		RestingHeartRate: 64, Conclusion: "eligible for gradual summer load",
	})
	if err != nil {
		f.t.Fatal(err)
	}
	f.athlete, err = f.profiles.Activate(ctx, f.advisor, athlete.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	if f.athlete.Status != domain.AthleteActive || f.athlete.Version != 3 {
		f.t.Fatalf("active athlete = %+v", f.athlete)
	}
	created, err := f.planning.CreatePrescription(ctx, f.coach, planningservice.PrescriptionInput{
		AthleteID: athlete.ID, WeekStart: f.now.Now(), WeeklyLoadLimit: 400,
		MaxSessionLoad: 400, MinRecoveryHours: 6, StrengthDays: 2, Basis: "baseline sequence 1",
		StrengthBlocks: []domain.StrengthBlock{
			{DayOffset: 1, MuscleGroup: "lower_body", Sets: 3, Repetitions: 10, IntensityRPE: 6},
			{DayOffset: 4, MuscleGroup: "upper_body", Sets: 2, Repetitions: 12, IntensityRPE: 5},
		},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	f.prescription, err = f.planning.PublishPrescription(ctx, f.coach, created.ID, created.Version)
	if err != nil {
		f.t.Fatal(err)
	}
}

func TestCompleteTrainingRiskAndRecoveryWorkflow(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := audit.WithRequestID(t.Context(), "workflow-main")
	for index := 0; index < 3; index++ {
		if index > 0 {
			f.now.Advance(24 * time.Hour)
		}
		occurred := f.now.Now()
		result, err := f.activities.Record(ctx, f.student, activityservice.RecordInput{
			AthleteID: f.athlete.ID, PrescriptionID: f.prescription.ID,
			IdempotencyKey: "session-" + string(rune('a'+index)), OccurredAt: occurred,
			DurationMinutes: 30, PerceivedEffort: 5, Source: domain.ActivityStudent,
		})
		if err != nil {
			t.Fatalf("activity %d: %v", index, err)
		}
		if result.Activity.LoadUnits != 150 || result.Replayed {
			t.Fatalf("activity %d result = %+v", index, result)
		}
		if index < 2 && result.Risk != nil {
			t.Fatalf("early risk at activity %d: %+v", index, result.Risk)
		}
		if index == 2 {
			if result.Risk == nil || result.Snapshot.SevenDayLoad != 450 {
				t.Fatalf("threshold result = %+v", result)
			}
		}
	}
	paused, err := f.database.AthleteByID(ctx, f.athlete.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != domain.AthletePaused || paused.Version != 4 {
		t.Fatalf("athlete was not atomically paused: %+v", paused)
	}
	if _, err = f.activities.Record(ctx, f.student, activityservice.RecordInput{
		AthleteID: f.athlete.ID, PrescriptionID: f.prescription.ID, IdempotencyKey: "blocked",
		OccurredAt: f.now.Now(), DurationMinutes: 10, PerceivedEffort: 2,
	}); !errors.Is(err, domain.ErrTrainingPaused) {
		t.Fatalf("activity while paused error = %v", err)
	}
	risks, err := f.database.ListRisks(ctx, f.athlete.ID, domain.RiskOpen, 10)
	if err != nil || len(risks) != 1 {
		t.Fatalf("open risks = %+v, %v", risks, err)
	}
	caseItem, err := f.risks.Acknowledge(ctx, f.advisor, risks[0].ID, risks[0].Version)
	if err != nil || caseItem.Status != domain.RiskAcknowledged {
		t.Fatalf("acknowledge = %+v, %v", caseItem, err)
	}
	caseItem, err = f.risks.Resolve(ctx, f.advisor, caseItem.ID, caseItem.Version, "reviewed history and reduced next load")
	if err != nil || caseItem.Status != domain.RiskResolved {
		t.Fatalf("resolve = %+v, %v", caseItem, err)
	}
	if _, err = f.risks.ResumeAthlete(ctx, f.advisor, f.athlete.ID, paused.Version, "risk resolved"); err == nil {
		t.Fatal("resume without reassessment succeeded")
	}
	reassessment, err := f.planning.RecordReassessment(ctx, f.advisor, domain.Reassessment{
		AthleteID: f.athlete.ID, BaselineID: f.baseline.ID, EnduranceScore: 62,
		StrengthScore: 60, MobilityScore: 71, Recommendation: "resume below prior weekly load",
		Basis: "stage retest after load hold", AssessedAt: f.now.Now(),
	})
	if err != nil || reassessment.ID == 0 {
		t.Fatalf("reassessment = %+v, %v", reassessment, err)
	}
	resumed, err := f.risks.ResumeAthlete(ctx, f.advisor, f.athlete.ID, paused.Version, "risk closed and retest current")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != domain.AthleteActive || resumed.Version != 5 || resumed.PausedReason != "" {
		t.Fatalf("resumed athlete = %+v", resumed)
	}
	audits, err := f.database.ListAudit(ctx, "athlete", f.athlete.ID, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) < 3 {
		t.Fatalf("athlete audit trail too short: %d", len(audits))
	}
}

func TestActivityIdempotencyReplayDoesNotDuplicateLoad(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := audit.WithRequestID(t.Context(), "idempotency")
	input := activityservice.RecordInput{AthleteID: f.athlete.ID, PrescriptionID: f.prescription.ID,
		IdempotencyKey: "device-sync-1", OccurredAt: f.now.Now(), DurationMinutes: 20,
		PerceivedEffort: 4, Source: domain.ActivityStudent}
	first, err := f.activities.Record(ctx, f.student, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.activities.Record(ctx, f.student, input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.Activity.ID != first.Activity.ID {
		t.Fatalf("replay = %+v", second)
	}
	var activities, snapshots int
	if err = f.database.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_logs`).Scan(&activities); err != nil {
		t.Fatal(err)
	}
	if err = f.database.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM load_snapshots`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if activities != 1 || snapshots != 1 {
		t.Fatalf("counts after replay = activities:%d snapshots:%d", activities, snapshots)
	}
	changed := input
	changed.DurationMinutes = 30
	if _, err = f.activities.Record(ctx, f.student, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
}

func TestFatigueReportCreatesRiskAndPersistentJob(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := audit.WithRequestID(t.Context(), "fatigue")
	report, riskCase, err := f.risks.SubmitFatigue(ctx, f.student, domain.FatigueReport{
		AthleteID: f.athlete.ID, ReportedFor: f.now.Now(), FatigueScore: 9,
		SorenessScore: 7, SleepHours: 4, Notes: "unusually exhausted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ID == 0 || riskCase == nil || riskCase.Severity != domain.RiskCritical {
		t.Fatalf("fatigue result = %+v risk=%+v", report, riskCase)
	}
	var pending int
	if err = f.database.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM worker_jobs WHERE status = 'pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending notification jobs = %d", pending)
	}
	athlete, err := f.database.AthleteByID(ctx, f.athlete.ID)
	if err != nil || athlete.Status != domain.AthletePaused {
		t.Fatalf("paused athlete = %+v, %v", athlete, err)
	}
	_, _, err = f.risks.SubmitFatigue(ctx, f.coach, domain.FatigueReport{AthleteID: f.athlete.ID,
		ReportedFor: f.now.Now().Add(time.Hour), FatigueScore: 5, SorenessScore: 5, SleepHours: 8})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("coach fatigue report error = %v", err)
	}
}

func TestProfessionalPermissionsAndVersionConflicts(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := audit.WithRequestID(t.Context(), "permissions")
	if _, err := f.planning.CreatePrescription(ctx, f.student, planningservice.PrescriptionInput{AthleteID: f.athlete.ID}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("student prescription error = %v", err)
	}
	if _, err := f.planning.PublishPrescription(ctx, f.coach, f.prescription.ID, 1); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale publish error = %v", err)
	}
	if _, err := f.profiles.GetAuthorized(ctx, domain.User{ID: 999, Role: domain.RoleGuardian}, f.athlete.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("unrelated guardian access error = %v", err)
	}
	if _, err := f.activities.Record(ctx, f.guardian, activityservice.RecordInput{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("guardian activity error = %v", err)
	}
}

// TestStalePublishLeavesActivePrescriptionIntact guards against a regression
// where a supersede committed in its own transaction before the version check,
// so a stale publish retired the active prescription without publishing a
// replacement and left the week with no executable prescription.
func TestStalePublishLeavesActivePrescriptionIntact(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := audit.WithRequestID(t.Context(), "stale-publish")
	// The active prescription was published at version 2; reporting the stale
	// version 1 must conflict without retiring the published one.
	if _, err := f.planning.PublishPrescription(ctx, f.coach, f.prescription.ID, 1); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale publish error = %v", err)
	}
	published, err := f.database.PublishedPrescription(ctx, f.athlete.ID, f.prescription.WeekStart)
	if err != nil {
		t.Fatalf("published prescription after stale publish = %v", err)
	}
	if published.ID != f.prescription.ID || published.Status != domain.PrescriptionPublished {
		t.Fatalf("active prescription was retired by a failed publish: %+v", published)
	}
	// The failed publish left no half-state: a new draft for the same week can
	// still be created and published, superseding the original atomically.
	draft, err := f.planning.CreatePrescription(ctx, f.coach, planningservice.PrescriptionInput{
		AthleteID: f.athlete.ID, WeekStart: f.prescription.WeekStart, WeeklyLoadLimit: 450,
		MaxSessionLoad: 400, MinRecoveryHours: 6, StrengthDays: 1, Basis: "summer progression",
	})
	if err != nil {
		t.Fatalf("create replacement draft = %v", err)
	}
	if _, err = f.planning.PublishPrescription(ctx, f.coach, draft.ID, draft.Version); err != nil {
		t.Fatalf("publish replacement draft = %v", err)
	}
	replacement, err := f.database.PublishedPrescription(ctx, f.athlete.ID, f.prescription.WeekStart)
	if err != nil {
		t.Fatalf("published replacement = %v", err)
	}
	if replacement.ID != draft.ID {
		t.Fatalf("replacement not published: got %+v", replacement)
	}
}
