package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
)

type Service struct {
	store *store.Store
	clock clock.Clock
	ttl   time.Duration
}

type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      domain.User `json:"user"`
}

func NewService(store *store.Store, clock clock.Clock, ttl time.Duration) *Service {
	return &Service{store: store, clock: clock, ttl: ttl}
}

func (s *Service) Register(ctx context.Context, email, displayName, password string, role domain.Role) (domain.User, error) {
	passwordHash, err := HashPassword(password)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", domain.FieldError{Field: "password", Problem: err.Error()})
	}
	now := s.clock.Now()
	user := domain.User{Email: email, DisplayName: displayName, PasswordHash: passwordHash, Role: role, CreatedAt: now}
	created, err := s.store.CreateUser(ctx, user)
	if err != nil {
		return domain.User{}, err
	}
	return created, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	user, err := s.store.UserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return LoginResult{}, domain.ErrUnauthorized
		}
		return LoginResult{}, err
	}
	if !user.Active || !VerifyPassword(user.PasswordHash, password) {
		return LoginResult{}, domain.ErrUnauthorized
	}
	plain, hash, err := NewSessionToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.clock.Now()
	session := domain.Session{UserID: user.ID, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(s.ttl)}
	created, err := s.store.CreateSession(ctx, session)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: plain, ExpiresAt: created.ExpiresAt, User: user}, nil
}

func (s *Service) Authenticate(ctx context.Context, plainToken string) (domain.User, error) {
	if strings.TrimSpace(plainToken) == "" {
		return domain.User{}, domain.ErrUnauthorized
	}
	session, user, err := s.store.SessionUser(ctx, HashToken(plainToken))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.User{}, domain.ErrUnauthorized
		}
		return domain.User{}, err
	}
	if !user.Active {
		return domain.User{}, domain.ErrUnauthorized
	}
	if err = session.Usable(s.clock.Now()); err != nil {
		return domain.User{}, fmt.Errorf("%w: %v", domain.ErrUnauthorized, err)
	}
	return user, nil
}

func (s *Service) Logout(ctx context.Context, plainToken string) error {
	if strings.TrimSpace(plainToken) == "" {
		return domain.ErrUnauthorized
	}
	return s.store.RevokeSession(ctx, HashToken(plainToken), s.clock.Now())
}

func (s *Service) PurgeExpired(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredSessions(ctx, s.clock.Now())
}
