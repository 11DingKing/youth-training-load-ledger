package domain

import (
	"strings"
	"time"
)

type Role string

const (
	RoleStudent  Role = "student"
	RoleGuardian Role = "guardian"
	RoleCoach    Role = "coach"
	RoleAdvisor  Role = "health_advisor"
)

func ParseRole(raw string) (Role, error) {
	role := Role(strings.TrimSpace(strings.ToLower(raw)))
	switch role {
	case RoleStudent, RoleGuardian, RoleCoach, RoleAdvisor:
		return role, nil
	default:
		return "", FieldError{Field: "role", Problem: "unsupported role"}
	}
}

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	Role         Role      `json:"role"`
	PasswordHash string    `json:"-"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (u User) CanManageTraining() bool {
	return u.Active && (u.Role == RoleCoach || u.Role == RoleAdvisor)
}

func (u User) CanResolveRisk() bool {
	return u.Active && u.Role == RoleAdvisor
}

type Session struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (s Session) Usable(now time.Time) error {
	if s.RevokedAt != nil {
		return ErrRevoked
	}
	if !now.Before(s.ExpiresAt) {
		return ErrExpired
	}
	return nil
}
