package domain

import (
	"errors"
	"testing"
	"time"
)

func TestAthleteAgeAtRespectsBirthday(t *testing.T) {
	birth := time.Date(2012, time.July, 10, 0, 0, 0, 0, time.UTC)
	athlete := Athlete{BirthDate: birth}
	before := time.Date(2026, time.July, 9, 12, 0, 0, 0, time.UTC)
	on := time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC)
	if got := athlete.AgeAt(before); got != 13 {
		t.Fatalf("age before birthday = %d, want 13", got)
	}
	if got := athlete.AgeAt(on); got != 14 {
		t.Fatalf("age on birthday = %d, want 14", got)
	}
}

func TestAthleteValidationRequiresMinorAndRelationships(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	valid := Athlete{StudentUserID: 1, GuardianUserID: 2,
		BirthDate: time.Date(2012, 8, 24, 0, 0, 0, 0, time.UTC), Timezone: "Asia/Shanghai"}
	if err := valid.Validate(now); err != nil {
		t.Fatalf("valid athlete: %v", err)
	}
	tests := []struct {
		name string
		edit func(*Athlete)
	}{
		{"missing student", func(a *Athlete) { a.StudentUserID = 0 }},
		{"same guardian", func(a *Athlete) { a.GuardianUserID = a.StudentUserID }},
		{"too young", func(a *Athlete) { a.BirthDate = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"adult", func(a *Athlete) { a.BirthDate = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"timezone", func(a *Athlete) { a.Timezone = "Mars/Olympus" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.edit(&candidate)
			if err := candidate.Validate(now); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestAthleteStateMachineTracksVersionAndReason(t *testing.T) {
	now := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	athlete := Athlete{Status: AthleteDraft, Version: 1}
	if err := athlete.Transition(AthleteAwaitingConsent, "consent requested", now); err != nil {
		t.Fatal(err)
	}
	if athlete.Version != 2 || athlete.Status != AthleteAwaitingConsent {
		t.Fatalf("after request = %+v", athlete)
	}
	if err := athlete.Transition(AthleteActive, "eligible", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := athlete.Transition(AthletePaused, "fatigue review", now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if athlete.PausedReason != "fatigue review" || athlete.Version != 4 {
		t.Fatalf("pause state = %+v", athlete)
	}
	if err := athlete.Transition(AthleteActive, "advisor clearance", now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if athlete.PausedReason != "" {
		t.Fatalf("pause reason retained after resume: %q", athlete.PausedReason)
	}
	if err := athlete.Transition(AthleteDraft, "rewind", now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("illegal rewind error = %v", err)
	}
}

func TestAthletePauseRequiresBasis(t *testing.T) {
	athlete := Athlete{Status: AthleteActive, Version: 5}
	err := athlete.Transition(AthletePaused, "", time.Now())
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("pause without reason error = %v", err)
	}
	if athlete.Status != AthleteActive || athlete.Version != 5 {
		t.Fatalf("invalid transition mutated athlete: %+v", athlete)
	}
}

func TestAthleteAuthorizationUsesAssignedRelationships(t *testing.T) {
	coachID, advisorID := int64(3), int64(4)
	athlete := Athlete{StudentUserID: 1, GuardianUserID: 2, CoachUserID: &coachID, AdvisorUserID: &advisorID}
	cases := []struct {
		user User
		want bool
	}{
		{User{ID: 1, Role: RoleStudent}, true},
		{User{ID: 2, Role: RoleGuardian}, true},
		{User{ID: 3, Role: RoleCoach}, true},
		{User{ID: 4, Role: RoleAdvisor}, true},
		{User{ID: 99, Role: RoleCoach}, false},
		{User{ID: 99, Role: RoleGuardian}, false},
		{User{ID: 99, Role: RoleStudent}, false},
		{User{ID: 99, Role: RoleAdvisor}, false},
	}
	for _, test := range cases {
		if got := athlete.Authorized(test.user); got != test.want {
			t.Errorf("Authorized(%+v) = %t, want %t", test.user, got, test.want)
		}
	}
	unassigned := athlete
	unassigned.AdvisorUserID = nil
	if !unassigned.Authorized(User{ID: 99, Role: RoleAdvisor}) {
		t.Fatal("unassigned athlete should be visible to an advisor")
	}
}

func TestGuardianConsentLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	consent := GuardianConsent{Status: ConsentPending}
	if err := consent.Grant(now, now.Add(30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !consent.ValidAt(now.Add(time.Hour)) {
		t.Fatal("granted consent not valid during effective window")
	}
	if consent.ValidAt(now.Add(30 * 24 * time.Hour)) {
		t.Fatal("consent valid at exclusive expiry")
	}
	if err := consent.Withdraw(now.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if consent.ValidAt(now.Add(3 * time.Hour)) {
		t.Fatal("withdrawn consent still valid")
	}
	if err := consent.Withdraw(now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second withdrawal error = %v", err)
	}
}

func TestConsentRejectsInvalidExpiry(t *testing.T) {
	now := time.Now().UTC()
	for _, expires := range []time.Time{now, now.Add(-time.Second)} {
		consent := GuardianConsent{Status: ConsentPending}
		if err := consent.Grant(now, expires); !errors.Is(err, ErrInvalid) {
			t.Errorf("Grant expiry %v error = %v", expires, err)
		}
		if consent.Status != ConsentPending {
			t.Errorf("invalid grant mutated status to %s", consent.Status)
		}
	}
}

func TestRoleParsingAndCapabilities(t *testing.T) {
	role, err := ParseRole(" HEALTH_ADVISOR ")
	if err != nil || role != RoleAdvisor {
		t.Fatalf("ParseRole = %q, %v", role, err)
	}
	if _, err = ParseRole("administrator"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported role error = %v", err)
	}
	advisor := User{Role: RoleAdvisor, Active: true}
	coach := User{Role: RoleCoach, Active: true}
	if !advisor.CanResolveRisk() || !advisor.CanManageTraining() {
		t.Fatal("advisor capabilities missing")
	}
	if coach.CanResolveRisk() || !coach.CanManageTraining() {
		t.Fatal("coach capabilities incorrect")
	}
	coach.Active = false
	if coach.CanManageTraining() {
		t.Fatal("inactive coach can manage training")
	}
}
