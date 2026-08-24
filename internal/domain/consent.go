package domain

import "time"

type ConsentStatus string

const (
	ConsentPending   ConsentStatus = "pending"
	ConsentGranted   ConsentStatus = "granted"
	ConsentWithdrawn ConsentStatus = "withdrawn"
	ConsentExpired   ConsentStatus = "expired"
)

type GuardianConsent struct {
	ID           int64         `json:"id"`
	AthleteID    int64         `json:"athlete_id"`
	GuardianID   int64         `json:"guardian_id"`
	Status       ConsentStatus `json:"status"`
	TermsVersion string        `json:"terms_version"`
	EffectiveAt  *time.Time    `json:"effective_at,omitempty"`
	ExpiresAt    *time.Time    `json:"expires_at,omitempty"`
	WithdrawnAt  *time.Time    `json:"withdrawn_at,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

func (c GuardianConsent) ValidAt(at time.Time) bool {
	if c.Status != ConsentGranted || c.EffectiveAt == nil || at.Before(*c.EffectiveAt) {
		return false
	}
	return c.ExpiresAt == nil || at.Before(*c.ExpiresAt)
}

func (c *GuardianConsent) Grant(at, expires time.Time) error {
	if c.Status != ConsentPending {
		return ErrInvalidState
	}
	if !expires.After(at) {
		return FieldError{Field: "expires_at", Problem: "must be after grant time"}
	}
	at = at.UTC()
	expires = expires.UTC()
	c.Status = ConsentGranted
	c.EffectiveAt = &at
	c.ExpiresAt = &expires
	return nil
}

func (c *GuardianConsent) Withdraw(at time.Time) error {
	if c.Status != ConsentGranted {
		return ErrInvalidState
	}
	at = at.UTC()
	c.Status = ConsentWithdrawn
	c.WithdrawnAt = &at
	return nil
}
