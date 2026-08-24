package domain

import (
	"fmt"
	"time"
)

type RiskStatus string

const (
	RiskOpen         RiskStatus = "open"
	RiskAcknowledged RiskStatus = "acknowledged"
	RiskResolved     RiskStatus = "resolved"
)

type RiskSeverity string

const (
	RiskModerate RiskSeverity = "moderate"
	RiskHigh     RiskSeverity = "high"
	RiskCritical RiskSeverity = "critical"
)

type RiskCase struct {
	ID                 int64        `json:"id"`
	AthleteID          int64        `json:"athlete_id"`
	TriggerType        string       `json:"trigger_type"`
	TriggerReferenceID int64        `json:"trigger_reference_id"`
	Severity           RiskSeverity `json:"severity"`
	Status             RiskStatus   `json:"status"`
	Basis              string       `json:"basis"`
	AssignedAdvisorID  *int64       `json:"assigned_advisor_id,omitempty"`
	Resolution         string       `json:"resolution,omitempty"`
	OpenedAt           time.Time    `json:"opened_at"`
	AcknowledgedAt     *time.Time   `json:"acknowledged_at,omitempty"`
	ResolvedAt         *time.Time   `json:"resolved_at,omitempty"`
	Version            int64        `json:"version"`
}

func (r *RiskCase) Acknowledge(advisor User, at time.Time) error {
	if advisor.Role != RoleAdvisor {
		return ErrForbidden
	}
	if r.Status != RiskOpen {
		return fmt.Errorf("%w: risk is %s", ErrInvalidState, r.Status)
	}
	at = at.UTC()
	r.Status = RiskAcknowledged
	r.AssignedAdvisorID = &advisor.ID
	r.AcknowledgedAt = &at
	r.Version++
	return nil
}

func (r *RiskCase) Resolve(advisor User, resolution string, at time.Time) error {
	if advisor.Role != RoleAdvisor || r.AssignedAdvisorID == nil || *r.AssignedAdvisorID != advisor.ID {
		return ErrForbidden
	}
	if r.Status != RiskAcknowledged {
		return fmt.Errorf("%w: risk must be acknowledged", ErrInvalidState)
	}
	if resolution == "" {
		return FieldError{Field: "resolution", Problem: "is required"}
	}
	at = at.UTC()
	r.Status = RiskResolved
	r.Resolution = resolution
	r.ResolvedAt = &at
	r.Version++
	return nil
}

type FatigueReport struct {
	ID             int64     `json:"id"`
	AthleteID      int64     `json:"athlete_id"`
	ReporterUserID int64     `json:"reporter_user_id"`
	ReportedFor    time.Time `json:"reported_for"`
	FatigueScore   int       `json:"fatigue_score"`
	SorenessScore  int       `json:"soreness_score"`
	SleepHours     float64   `json:"sleep_hours"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
}

func (f FatigueReport) Validate() error {
	if f.FatigueScore < 1 || f.FatigueScore > 10 || f.SorenessScore < 1 || f.SorenessScore > 10 {
		return FieldError{Field: "scores", Problem: "fatigue and soreness must be between 1 and 10"}
	}
	if f.SleepHours < 0 || f.SleepHours > 16 {
		return FieldError{Field: "sleep_hours", Problem: "must be between 0 and 16"}
	}
	return nil
}

func (f FatigueReport) Severity() (RiskSeverity, bool) {
	switch {
	case f.FatigueScore >= 9 || f.SorenessScore >= 9 || f.SleepHours < 3:
		return RiskCritical, true
	case f.FatigueScore >= 7 || f.SorenessScore >= 7 || f.SleepHours < 5:
		return RiskHigh, true
	case f.FatigueScore >= 6 && f.SorenessScore >= 6:
		return RiskModerate, true
	default:
		return "", false
	}
}

type Reassessment struct {
	ID             int64     `json:"id"`
	AthleteID      int64     `json:"athlete_id"`
	AssessorUserID int64     `json:"assessor_user_id"`
	BaselineID     int64     `json:"baseline_id"`
	EnduranceScore int       `json:"endurance_score"`
	StrengthScore  int       `json:"strength_score"`
	MobilityScore  int       `json:"mobility_score"`
	Recommendation string    `json:"recommendation"`
	Basis          string    `json:"basis"`
	AssessedAt     time.Time `json:"assessed_at"`
}

func (r Reassessment) Validate() error {
	baseline := BaselineAssessment{
		AthleteID: r.AthleteID, AssessorUserID: r.AssessorUserID,
		EnduranceScore: r.EnduranceScore, StrengthScore: r.StrengthScore,
		MobilityScore: r.MobilityScore, RestingHeartRate: 60,
		Conclusion: r.Recommendation,
	}
	if r.BaselineID <= 0 {
		return FieldError{Field: "baseline_id", Problem: "is required"}
	}
	if err := baseline.Validate(); err != nil {
		return err
	}
	if r.Basis == "" {
		return FieldError{Field: "basis", Problem: "is required"}
	}
	return nil
}
