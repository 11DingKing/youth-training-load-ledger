package domain

import "errors"

var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrInvalid          = errors.New("invalid input")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrExpired          = errors.New("expired")
	ErrRevoked          = errors.New("revoked")
	ErrVersionConflict  = errors.New("version conflict")
	ErrInvalidState     = errors.New("invalid state transition")
	ErrConsentRequired  = errors.New("guardian consent required")
	ErrBaselineRequired = errors.New("baseline assessment required")
	ErrRiskOpen         = errors.New("open risk case prevents operation")
	ErrTrainingPaused   = errors.New("training is paused")
	ErrLoadExceeded     = errors.New("training load threshold exceeded")
)

type FieldError struct {
	Field   string
	Problem string
}

func (e FieldError) Error() string {
	if e.Field == "" {
		return e.Problem
	}
	return e.Field + ": " + e.Problem
}

func (e FieldError) Unwrap() error { return ErrInvalid }
