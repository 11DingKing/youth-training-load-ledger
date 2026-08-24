package domain

import "time"

type ScreeningDecision string

const (
	ScreeningPending ScreeningDecision = "pending"
	ScreeningCleared ScreeningDecision = "cleared"
	ScreeningReview  ScreeningDecision = "review_required"
)

type HealthScreening struct {
	ID             int64             `json:"id"`
	AthleteID      int64             `json:"athlete_id"`
	AnswersJSON    string            `json:"answers_json"`
	Decision       ScreeningDecision `json:"decision"`
	ReviewerUserID *int64            `json:"reviewer_user_id,omitempty"`
	ReviewBasis    string            `json:"review_basis,omitempty"`
	SubmittedAt    time.Time         `json:"submitted_at"`
	ReviewedAt     *time.Time        `json:"reviewed_at,omitempty"`
}

func (s *HealthScreening) Review(reviewer User, clear bool, basis string, at time.Time) error {
	if reviewer.Role != RoleAdvisor {
		return ErrForbidden
	}
	if s.Decision != ScreeningPending && s.Decision != ScreeningReview {
		return ErrInvalidState
	}
	if basis == "" {
		return FieldError{Field: "basis", Problem: "professional basis is required"}
	}
	s.ReviewerUserID = &reviewer.ID
	s.ReviewBasis = basis
	at = at.UTC()
	s.ReviewedAt = &at
	if clear {
		s.Decision = ScreeningCleared
	} else {
		s.Decision = ScreeningReview
	}
	return nil
}

type BaselineAssessment struct {
	ID               int64     `json:"id"`
	AthleteID        int64     `json:"athlete_id"`
	AssessorUserID   int64     `json:"assessor_user_id"`
	Sequence         int       `json:"sequence"`
	EnduranceScore   int       `json:"endurance_score"`
	StrengthScore    int       `json:"strength_score"`
	MobilityScore    int       `json:"mobility_score"`
	RestingHeartRate int       `json:"resting_heart_rate"`
	Conclusion       string    `json:"conclusion"`
	AssessedAt       time.Time `json:"assessed_at"`
}

func (b BaselineAssessment) Validate() error {
	if b.AthleteID <= 0 || b.AssessorUserID <= 0 {
		return FieldError{Field: "assessment", Problem: "athlete and assessor are required"}
	}
	for name, score := range map[string]int{"endurance": b.EnduranceScore, "strength": b.StrengthScore, "mobility": b.MobilityScore} {
		if score < 0 || score > 100 {
			return FieldError{Field: name + "_score", Problem: "must be between 0 and 100"}
		}
	}
	if b.RestingHeartRate < 35 || b.RestingHeartRate > 160 {
		return FieldError{Field: "resting_heart_rate", Problem: "outside accepted measurement range"}
	}
	if b.Conclusion == "" {
		return FieldError{Field: "conclusion", Problem: "is required"}
	}
	return nil
}
