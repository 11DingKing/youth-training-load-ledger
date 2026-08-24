package domain

import (
	"fmt"
	"time"
)

type ActivitySource string

const (
	ActivityStudent    ActivitySource = "student"
	ActivityCoach      ActivitySource = "coach"
	ActivityCorrection ActivitySource = "correction"
)

type ActivityLog struct {
	ID              int64          `json:"id"`
	AthleteID       int64          `json:"athlete_id"`
	PrescriptionID  int64          `json:"prescription_id"`
	RecorderUserID  int64          `json:"recorder_user_id"`
	IdempotencyKey  string         `json:"idempotency_key"`
	OccurredAt      time.Time      `json:"occurred_at"`
	RecordedAt      time.Time      `json:"recorded_at"`
	DurationMinutes int            `json:"duration_minutes"`
	PerceivedEffort int            `json:"perceived_effort"`
	LoadUnits       int            `json:"load_units"`
	Source          ActivitySource `json:"source"`
	LateEntry       bool           `json:"late_entry"`
	SupersedesID    *int64         `json:"supersedes_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

func CalculateLoad(durationMinutes, perceivedEffort int) (int, error) {
	if durationMinutes < 1 || durationMinutes > 360 {
		return 0, FieldError{Field: "duration_minutes", Problem: "must be between 1 and 360"}
	}
	if perceivedEffort < 1 || perceivedEffort > 10 {
		return 0, FieldError{Field: "perceived_effort", Problem: "must be between 1 and 10"}
	}
	return durationMinutes * perceivedEffort, nil
}

func (a *ActivityLog) Validate(now time.Time) error {
	if a.AthleteID <= 0 || a.PrescriptionID <= 0 || a.RecorderUserID <= 0 {
		return FieldError{Field: "activity", Problem: "athlete, prescription and recorder are required"}
	}
	if a.IdempotencyKey == "" {
		return FieldError{Field: "idempotency_key", Problem: "is required"}
	}
	if a.OccurredAt.After(now.Add(5 * time.Minute)) {
		return FieldError{Field: "occurred_at", Problem: "cannot be in the future"}
	}
	load, err := CalculateLoad(a.DurationMinutes, a.PerceivedEffort)
	if err != nil {
		return err
	}
	a.LoadUnits = load
	a.LateEntry = now.Sub(a.OccurredAt) > 24*time.Hour
	if a.Source == ActivityCorrection && a.SupersedesID == nil {
		return FieldError{Field: "supersedes_id", Problem: "correction must reference an earlier activity"}
	}
	return nil
}

type LoadSnapshot struct {
	ID            int64     `json:"id"`
	AthleteID     int64     `json:"athlete_id"`
	ActivityID    int64     `json:"activity_id"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	SevenDayLoad  int       `json:"seven_day_load"`
	PreviousLoad  int       `json:"previous_load"`
	Threshold     int       `json:"threshold"`
	RiskTriggered bool      `json:"risk_triggered"`
	CalculatedAt  time.Time `json:"calculated_at"`
}

func BuildLoadSnapshot(activity ActivityLog, prior []ActivityLog, limit int, at time.Time) (LoadSnapshot, error) {
	if limit <= 0 {
		return LoadSnapshot{}, fmt.Errorf("%w: load limit must be positive", ErrInvalid)
	}
	start := activity.OccurredAt.Add(-6 * 24 * time.Hour)
	total := activity.LoadUnits
	previous := 0
	for _, item := range prior {
		if item.ID == activity.ID || item.Source == ActivityCorrection {
			continue
		}
		if !item.OccurredAt.Before(start) && !item.OccurredAt.After(activity.OccurredAt) {
			total += item.LoadUnits
		}
		if item.OccurredAt.Before(activity.OccurredAt) {
			previous += item.LoadUnits
		}
	}
	return LoadSnapshot{
		AthleteID: activity.AthleteID, ActivityID: activity.ID,
		WindowStart: start, WindowEnd: activity.OccurredAt,
		SevenDayLoad: total, PreviousLoad: previous, Threshold: limit,
		RiskTriggered: total > limit || activity.LoadUnits > limit/2,
		CalculatedAt:  at.UTC(),
	}, nil
}
