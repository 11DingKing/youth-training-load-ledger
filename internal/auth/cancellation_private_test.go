package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestCanceledAuthenticationStopsBeforeReturningUser(t *testing.T) {
	database := openAuthStore(t)
	service := NewService(database, clock.NewFixed(time.Now().UTC()), time.Hour)
	user, err := service.Register(t.Context(), "cancel@example.test", "Canceled", "strong-password", domain.RoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(t.Context(), user.Email, "strong-password")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	authenticated, err := service.Authenticate(ctx, login.Token)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Authenticate error = %v, want context canceled", err)
	}
	if authenticated.ID != 0 {
		t.Fatalf("canceled authentication returned user %d", authenticated.ID)
	}
}
