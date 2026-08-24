package domain

import (
	"fmt"
	"time"
)

type AthleteStatus string

const (
	AthleteDraft           AthleteStatus = "draft"
	AthleteAwaitingConsent AthleteStatus = "awaiting_consent"
	AthleteActive          AthleteStatus = "active"
	AthletePaused          AthleteStatus = "paused"
	AthleteClosed          AthleteStatus = "closed"
)

type Athlete struct {
	ID             int64         `json:"id"`
	StudentUserID  int64         `json:"student_user_id"`
	GuardianUserID int64         `json:"guardian_user_id"`
	CoachUserID    *int64        `json:"coach_user_id,omitempty"`
	AdvisorUserID  *int64        `json:"advisor_user_id,omitempty"`
	BirthDate      time.Time     `json:"birth_date"`
	Timezone       string        `json:"timezone"`
	Status         AthleteStatus `json:"status"`
	Version        int64         `json:"version"`
	PausedReason   string        `json:"paused_reason,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func (a Athlete) AgeAt(at time.Time) int {
	age := at.Year() - a.BirthDate.Year()
	anniversary := time.Date(at.Year(), a.BirthDate.Month(), a.BirthDate.Day(), 0, 0, 0, 0, at.Location())
	if at.Before(anniversary) {
		age--
	}
	return age
}

func (a Athlete) Validate(at time.Time) error {
	if a.StudentUserID <= 0 || a.GuardianUserID <= 0 || a.StudentUserID == a.GuardianUserID {
		return FieldError{Field: "relationships", Problem: "student and guardian are required and distinct"}
	}
	if age := a.AgeAt(at); age < 10 || age > 17 {
		return FieldError{Field: "birth_date", Problem: "athlete must be 10 through 17"}
	}
	if _, err := time.LoadLocation(a.Timezone); err != nil {
		return FieldError{Field: "timezone", Problem: "unknown IANA timezone"}
	}
	return nil
}

func (a Athlete) CanTransition(next AthleteStatus) bool {
	switch a.Status {
	case AthleteDraft:
		return next == AthleteAwaitingConsent
	case AthleteAwaitingConsent:
		return next == AthleteActive || next == AthleteClosed
	case AthleteActive:
		return next == AthletePaused || next == AthleteClosed
	case AthletePaused:
		return next == AthleteActive || next == AthleteClosed
	default:
		return false
	}
}

func (a *Athlete) Transition(next AthleteStatus, reason string, now time.Time) error {
	if !a.CanTransition(next) {
		return fmt.Errorf("%w: athlete %s to %s", ErrInvalidState, a.Status, next)
	}
	if next == AthletePaused && reason == "" {
		return FieldError{Field: "reason", Problem: "pause reason is required"}
	}
	a.Status = next
	a.PausedReason = ""
	if next == AthletePaused {
		a.PausedReason = reason
	}
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}

func (a Athlete) Authorized(user User) bool {
	if user.Role == RoleAdvisor {
		return a.AdvisorUserID == nil || *a.AdvisorUserID == user.ID
	}
	if user.Role == RoleCoach {
		return a.CoachUserID != nil && *a.CoachUserID == user.ID
	}
	if user.Role == RoleGuardian {
		return a.GuardianUserID == user.ID
	}
	return user.Role == RoleStudent && a.StudentUserID == user.ID
}
