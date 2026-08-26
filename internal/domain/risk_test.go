package domain

import (
	"errors"
	"testing"
	"time"
)

func TestScreeningReviewRequiresAdvisorAndBasis(t *testing.T) {
	now := time.Now().UTC()
	screening := HealthScreening{Decision: ScreeningReview}
	coach := User{ID: 2, Role: RoleCoach}
	if err := screening.Review(coach, true, "coach opinion", now); !errors.Is(err, ErrForbidden) {
		t.Fatalf("coach review error = %v", err)
	}
	advisor := User{ID: 3, Role: RoleAdvisor}
	if err := screening.Review(advisor, true, "", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty basis error = %v", err)
	}
	if err := screening.Review(advisor, true, "screening evidence reviewed", now); err != nil {
		t.Fatal(err)
	}
	if screening.Decision != ScreeningCleared || screening.ReviewerUserID == nil || *screening.ReviewerUserID != advisor.ID {
		t.Fatalf("reviewed screening = %+v", screening)
	}
	if err := screening.Review(advisor, false, "reopen", now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("review terminal screening error = %v", err)
	}
}

func TestBaselineAssessmentValidation(t *testing.T) {
	valid := BaselineAssessment{AthleteID: 1, AssessorUserID: 2, EnduranceScore: 60,
		StrengthScore: 55, MobilityScore: 70, RestingHeartRate: 65, Conclusion: "cleared for progressive plan"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*BaselineAssessment)
	}{
		{"athlete", func(b *BaselineAssessment) { b.AthleteID = 0 }},
		{"endurance low", func(b *BaselineAssessment) { b.EnduranceScore = -1 }},
		{"strength high", func(b *BaselineAssessment) { b.StrengthScore = 101 }},
		{"mobility high", func(b *BaselineAssessment) { b.MobilityScore = 101 }},
		{"heart rate low", func(b *BaselineAssessment) { b.RestingHeartRate = 34 }},
		{"heart rate high", func(b *BaselineAssessment) { b.RestingHeartRate = 161 }},
		{"conclusion", func(b *BaselineAssessment) { b.Conclusion = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.edit(&candidate)
			if err := candidate.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRiskCaseLifecycleEnforcesAssignmentAndVersion(t *testing.T) {
	now := time.Now().UTC()
	risk := RiskCase{Status: RiskOpen, Version: 1}
	coach := User{ID: 8, Role: RoleCoach}
	if err := risk.Acknowledge(coach, now); !errors.Is(err, ErrForbidden) {
		t.Fatalf("coach acknowledge = %v", err)
	}
	first := User{ID: 10, Role: RoleAdvisor}
	second := User{ID: 11, Role: RoleAdvisor}
	if err := risk.Acknowledge(first, now); err != nil {
		t.Fatal(err)
	}
	if risk.Status != RiskAcknowledged || risk.Version != 2 || risk.AssignedAdvisorID == nil {
		t.Fatalf("acknowledged risk = %+v", risk)
	}
	if err := risk.Resolve(second, "different advisor", now); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other advisor resolution = %v", err)
	}
	if err := risk.Resolve(first, "", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty resolution = %v", err)
	}
	if err := risk.Resolve(first, "load reviewed and new baseline completed", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if risk.Status != RiskResolved || risk.Version != 3 || risk.ResolvedAt == nil {
		t.Fatalf("resolved risk = %+v", risk)
	}
	if err := risk.Resolve(first, "again", now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second resolution = %v", err)
	}
}

func TestFatigueReportSeverity(t *testing.T) {
	tests := []struct {
		name     string
		report   FatigueReport
		severity RiskSeverity
		risky    bool
	}{
		{"normal", FatigueReport{FatigueScore: 4, SorenessScore: 4, SleepHours: 8}, "", false},
		{"combined moderate", FatigueReport{FatigueScore: 6, SorenessScore: 6, SleepHours: 8}, RiskModerate, true},
		{"high fatigue", FatigueReport{FatigueScore: 7, SorenessScore: 3, SleepHours: 8}, RiskHigh, true},
		{"low sleep", FatigueReport{FatigueScore: 2, SorenessScore: 2, SleepHours: 4.5}, RiskHigh, true},
		{"critical fatigue", FatigueReport{FatigueScore: 9, SorenessScore: 2, SleepHours: 8}, RiskCritical, true},
		{"critical sleep", FatigueReport{FatigueScore: 2, SorenessScore: 2, SleepHours: 2.5}, RiskCritical, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			severity, risky := test.report.Severity()
			if severity != test.severity || risky != test.risky {
				t.Fatalf("Severity() = %q,%t want %q,%t", severity, risky, test.severity, test.risky)
			}
		})
	}
}

func TestFatigueValidationBoundaries(t *testing.T) {
	valid := FatigueReport{FatigueScore: 5, SorenessScore: 5, SleepHours: 7.5}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := []FatigueReport{
		{FatigueScore: 0, SorenessScore: 5, SleepHours: 8},
		{FatigueScore: 5, SorenessScore: 11, SleepHours: 8},
		{FatigueScore: 5, SorenessScore: 5, SleepHours: -1},
		{FatigueScore: 5, SorenessScore: 5, SleepHours: 17},
	}
	for index, report := range invalid {
		if err := report.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d error = %v", index, err)
		}
	}
}

func TestReassessmentRequiresBaselineAndBasis(t *testing.T) {
	valid := Reassessment{AthleteID: 1, AssessorUserID: 2, BaselineID: 3,
		EnduranceScore: 65, StrengthScore: 60, MobilityScore: 70,
		Recommendation: "resume with reduced load", Basis: "stage retest"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	missingBaseline := valid
	missingBaseline.BaselineID = 0
	if err := missingBaseline.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing baseline error = %v", err)
	}
	missingBasis := valid
	missingBasis.Basis = ""
	if err := missingBasis.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing basis error = %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	now := time.Now().UTC()
	session := Session{ExpiresAt: now.Add(time.Hour)}
	if err := session.Usable(now); err != nil {
		t.Fatal(err)
	}
	if err := session.Usable(now.Add(time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry error = %v", err)
	}
	revoked := now.Add(-time.Minute)
	session.RevokedAt = &revoked
	if err := session.Usable(now); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked error = %v", err)
	}
}

func TestAuditValidation(t *testing.T) {
	valid := AuditEvent{ActorID: 1, Action: "athlete.pause", ObjectType: "athlete", ObjectID: 2,
		Outcome: "success", RequestID: "request-1"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*AuditEvent){
		"actor":   func(a *AuditEvent) { a.ActorID = 0 },
		"action":  func(a *AuditEvent) { a.Action = "" },
		"object":  func(a *AuditEvent) { a.ObjectID = 0 },
		"outcome": func(a *AuditEvent) { a.Outcome = "unknown" },
		"request": func(a *AuditEvent) { a.RequestID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			edit(&candidate)
			if err := candidate.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestWorkerJobEligibility(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	tests := []struct {
		job  WorkerJob
		want bool
	}{
		{WorkerJob{Status: JobPending, AvailableAt: past}, true},
		{WorkerJob{Status: JobRetry, AvailableAt: past}, true},
		{WorkerJob{Status: JobRunning, AvailableAt: past, LeaseUntil: &past}, true},
		{WorkerJob{Status: JobRunning, AvailableAt: past, LeaseUntil: &future}, false},
		{WorkerJob{Status: JobPending, AvailableAt: future}, false},
		{WorkerJob{Status: JobSucceeded, AvailableAt: past}, false},
		{WorkerJob{Status: JobPermanentFailed, AvailableAt: past}, false},
	}
	for index, test := range tests {
		if got := test.job.CanRun(now); got != test.want {
			t.Errorf("case %d CanRun() = %t, want %t", index, got, test.want)
		}
	}
}
