package domain

import (
	"fmt"
	"time"
)

type PrescriptionStatus string

const (
	PrescriptionDraft      PrescriptionStatus = "draft"
	PrescriptionPublished  PrescriptionStatus = "published"
	PrescriptionSuperseded PrescriptionStatus = "superseded"
)

type Prescription struct {
	ID               int64              `json:"id"`
	AthleteID        int64              `json:"athlete_id"`
	AuthorUserID     int64              `json:"author_user_id"`
	WeekStart        time.Time          `json:"week_start"`
	Version          int64              `json:"version"`
	Status           PrescriptionStatus `json:"status"`
	WeeklyLoadLimit  int                `json:"weekly_load_limit"`
	MaxSessionLoad   int                `json:"max_session_load"`
	MinRecoveryHours int                `json:"min_recovery_hours"`
	StrengthDays     int                `json:"strength_days"`
	Basis            string             `json:"basis"`
	PublishedAt      *time.Time         `json:"published_at,omitempty"`
	SupersededAt     *time.Time         `json:"superseded_at,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
}

func NormalizeWeekStart(t time.Time, location *time.Location) time.Time {
	local := t.In(location)
	delta := (int(local.Weekday()) + 6) % 7
	return time.Date(local.Year(), local.Month(), local.Day()-delta, 0, 0, 0, 0, location).UTC()
}

func (p Prescription) Validate() error {
	if p.AthleteID <= 0 || p.AuthorUserID <= 0 {
		return FieldError{Field: "prescription", Problem: "athlete and author are required"}
	}
	if p.WeeklyLoadLimit < 100 || p.WeeklyLoadLimit > 5000 {
		return FieldError{Field: "weekly_load_limit", Problem: "must be between 100 and 5000"}
	}
	if p.MaxSessionLoad < 20 || p.MaxSessionLoad > p.WeeklyLoadLimit {
		return FieldError{Field: "max_session_load", Problem: "must be positive and within weekly limit"}
	}
	if p.MinRecoveryHours < 6 || p.MinRecoveryHours > 96 {
		return FieldError{Field: "min_recovery_hours", Problem: "must be between 6 and 96"}
	}
	if p.StrengthDays < 0 || p.StrengthDays > 4 {
		return FieldError{Field: "strength_days", Problem: "must be between 0 and 4"}
	}
	if p.Basis == "" {
		return FieldError{Field: "basis", Problem: "professional basis is required"}
	}
	return nil
}

func (p *Prescription) Publish(at time.Time) error {
	if p.Status != PrescriptionDraft {
		return fmt.Errorf("%w: prescription is %s", ErrInvalidState, p.Status)
	}
	at = at.UTC()
	p.Status = PrescriptionPublished
	p.PublishedAt = &at
	return nil
}

func (p *Prescription) Supersede(at time.Time) error {
	if p.Status != PrescriptionPublished {
		return fmt.Errorf("%w: only published prescriptions can be superseded", ErrInvalidState)
	}
	at = at.UTC()
	p.Status = PrescriptionSuperseded
	p.SupersededAt = &at
	return nil
}

type StrengthBlock struct {
	ID             int64     `json:"id"`
	PrescriptionID int64     `json:"prescription_id"`
	DayOffset      int       `json:"day_offset"`
	MuscleGroup    string    `json:"muscle_group"`
	Sets           int       `json:"sets"`
	Repetitions    int       `json:"repetitions"`
	IntensityRPE   int       `json:"intensity_rpe"`
	CreatedAt      time.Time `json:"created_at"`
}

func (b StrengthBlock) Validate() error {
	if b.DayOffset < 0 || b.DayOffset > 6 {
		return FieldError{Field: "day_offset", Problem: "must be within prescription week"}
	}
	if b.MuscleGroup == "" || b.Sets < 1 || b.Sets > 10 || b.Repetitions < 1 || b.Repetitions > 50 {
		return FieldError{Field: "strength_block", Problem: "invalid muscle group, sets, or repetitions"}
	}
	if b.IntensityRPE < 1 || b.IntensityRPE > 10 {
		return FieldError{Field: "intensity_rpe", Problem: "must be between 1 and 10"}
	}
	return nil
}
