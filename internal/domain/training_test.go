package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeWeekStartAcrossTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	wednesday := time.Date(2026, 8, 26, 18, 30, 0, 0, location)
	got := NormalizeWeekStart(wednesday, location)
	want := time.Date(2026, 8, 24, 0, 0, 0, 0, location).UTC()
	if !got.Equal(want) {
		t.Fatalf("week start = %s, want %s", got, want)
	}
	sunday := time.Date(2026, 8, 30, 23, 59, 0, 0, location)
	if got := NormalizeWeekStart(sunday, location); !got.Equal(want) {
		t.Fatalf("Sunday week start = %s, want %s", got, want)
	}
}

func TestPrescriptionValidationBoundaries(t *testing.T) {
	valid := Prescription{AthleteID: 1, AuthorUserID: 2, WeeklyLoadLimit: 1000,
		MaxSessionLoad: 300, MinRecoveryHours: 24, StrengthDays: 2, Basis: "baseline assessment"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*Prescription)
	}{
		{"identity", func(p *Prescription) { p.AthleteID = 0 }},
		{"weekly low", func(p *Prescription) { p.WeeklyLoadLimit = 99 }},
		{"weekly high", func(p *Prescription) { p.WeeklyLoadLimit = 5001 }},
		{"session low", func(p *Prescription) { p.MaxSessionLoad = 19 }},
		{"session over week", func(p *Prescription) { p.MaxSessionLoad = 1001 }},
		{"recovery low", func(p *Prescription) { p.MinRecoveryHours = 5 }},
		{"recovery high", func(p *Prescription) { p.MinRecoveryHours = 97 }},
		{"strength days", func(p *Prescription) { p.StrengthDays = 5 }},
		{"basis", func(p *Prescription) { p.Basis = "" }},
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

func TestPrescriptionLifecycle(t *testing.T) {
	now := time.Now().UTC()
	p := Prescription{Status: PrescriptionDraft}
	if err := p.Publish(now); err != nil {
		t.Fatal(err)
	}
	if p.Status != PrescriptionPublished || p.PublishedAt == nil || !p.PublishedAt.Equal(now) {
		t.Fatalf("published = %+v", p)
	}
	if err := p.Publish(now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second publish = %v", err)
	}
	if err := p.Supersede(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if p.Status != PrescriptionSuperseded || p.SupersededAt == nil {
		t.Fatalf("superseded = %+v", p)
	}
	if err := p.Supersede(now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second supersede = %v", err)
	}
}

func TestStrengthBlockValidation(t *testing.T) {
	valid := StrengthBlock{DayOffset: 3, MuscleGroup: "posterior_chain", Sets: 3, Repetitions: 10, IntensityRPE: 6}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []StrengthBlock{
		{DayOffset: -1, MuscleGroup: "legs", Sets: 1, Repetitions: 1, IntensityRPE: 1},
		{DayOffset: 7, MuscleGroup: "legs", Sets: 1, Repetitions: 1, IntensityRPE: 1},
		{DayOffset: 1, MuscleGroup: "", Sets: 1, Repetitions: 1, IntensityRPE: 1},
		{DayOffset: 1, MuscleGroup: "legs", Sets: 0, Repetitions: 1, IntensityRPE: 1},
		{DayOffset: 1, MuscleGroup: "legs", Sets: 1, Repetitions: 51, IntensityRPE: 1},
		{DayOffset: 1, MuscleGroup: "legs", Sets: 1, Repetitions: 10, IntensityRPE: 11},
	}
	for index, block := range tests {
		if err := block.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d error = %v", index, err)
		}
	}
}

func TestCalculateLoadValidatesInputs(t *testing.T) {
	load, err := CalculateLoad(45, 7)
	if err != nil || load != 315 {
		t.Fatalf("CalculateLoad = %d, %v", load, err)
	}
	for _, input := range [][2]int{{0, 5}, {361, 5}, {30, 0}, {30, 11}} {
		if _, err := CalculateLoad(input[0], input[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("CalculateLoad(%d,%d) error = %v", input[0], input[1], err)
		}
	}
}

func TestActivityValidationCalculatesLateLoad(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	activity := ActivityLog{AthleteID: 1, PrescriptionID: 2, RecorderUserID: 3,
		IdempotencyKey: "day-1", OccurredAt: now.Add(-25 * time.Hour), DurationMinutes: 60,
		PerceivedEffort: 6, Source: ActivityStudent}
	if err := activity.Validate(now); err != nil {
		t.Fatal(err)
	}
	if activity.LoadUnits != 360 || !activity.LateEntry {
		t.Fatalf("activity = %+v", activity)
	}
	activity.OccurredAt = now.Add(6 * time.Minute)
	if err := activity.Validate(now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("future activity error = %v", err)
	}
}

func TestCorrectionRequiresOriginalReference(t *testing.T) {
	activity := ActivityLog{AthleteID: 1, PrescriptionID: 2, RecorderUserID: 3,
		IdempotencyKey: "correction", OccurredAt: time.Now().Add(-time.Hour),
		DurationMinutes: 10, PerceivedEffort: 3, Source: ActivityCorrection}
	if err := activity.Validate(time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("correction error = %v", err)
	}
	original := int64(9)
	activity.SupersedesID = &original
	if err := activity.Validate(time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestBuildLoadSnapshotUsesHistoricalWindow(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	current := ActivityLog{ID: 10, AthleteID: 1, OccurredAt: now, LoadUnits: 400}
	prior := []ActivityLog{
		{ID: 1, AthleteID: 1, OccurredAt: now.Add(-24 * time.Hour), LoadUnits: 300},
		{ID: 2, AthleteID: 1, OccurredAt: now.Add(-5 * 24 * time.Hour), LoadUnits: 250},
		{ID: 3, AthleteID: 1, OccurredAt: now.Add(-8 * 24 * time.Hour), LoadUnits: 900},
		{ID: 4, AthleteID: 1, OccurredAt: now.Add(-2 * time.Hour), LoadUnits: 100, Source: ActivityCorrection},
	}
	snapshot, err := BuildLoadSnapshot(current, prior, 900, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SevenDayLoad != 950 || snapshot.PreviousLoad != 1450 {
		t.Fatalf("snapshot loads = %+v", snapshot)
	}
	if !snapshot.RiskTriggered {
		t.Fatal("over-threshold snapshot did not trigger risk")
	}
	if !snapshot.WindowStart.Equal(now.Add(-6 * 24 * time.Hour)) {
		t.Fatalf("window start = %s", snapshot.WindowStart)
	}
}

func TestBuildLoadSnapshotRejectsInvalidLimit(t *testing.T) {
	_, err := BuildLoadSnapshot(ActivityLog{}, nil, 0, time.Now())
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid limit error = %v", err)
	}
}
