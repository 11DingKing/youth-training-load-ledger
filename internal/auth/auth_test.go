package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
)

func openAuthStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := store.Open(t.Context(), "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestPasswordHashRoundTripAndSalt(t *testing.T) {
	first, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes reused salt")
	}
	if !strings.HasPrefix(first, "pbkdf2-sha256$210000$") {
		t.Fatalf("unexpected hash format: %s", first)
	}
	if !VerifyPassword(first, "correct horse battery staple") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(first, "wrong password") {
		t.Fatal("wrong password accepted")
	}
}

func TestPasswordHashRejectsWeakLength(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := HashPassword(strings.Repeat("x", 129)); err == nil {
		t.Fatal("oversized password accepted")
	}
	for _, malformed := range []string{"", "sha256$x", "pbkdf2-sha256$x$a$b", "pbkdf2-sha256$1$a$b"} {
		if VerifyPassword(malformed, "password") {
			t.Errorf("malformed hash accepted: %q", malformed)
		}
	}
}

func TestSessionTokenIsRandomAndHashStable(t *testing.T) {
	first, firstHash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || firstHash == secondHash {
		t.Fatal("session tokens are not unique")
	}
	if HashToken(first) != firstHash || len(firstHash) != 64 {
		t.Fatalf("token hash mismatch: %s", firstHash)
	}
}

func TestServiceLoginAuthenticateLogout(t *testing.T) {
	database := openAuthStore(t)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	fixed := clock.NewFixed(now)
	service := NewService(database, fixed, 2*time.Hour)
	user, err := service.Register(t.Context(), "student@example.test", "Student", "strong-password", domain.RoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(t.Context(), " STUDENT@example.test ", "strong-password")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.ID != user.ID || !result.ExpiresAt.Equal(now.Add(2*time.Hour)) || result.Token == "" {
		t.Fatalf("login result = %+v", result)
	}
	authenticated, err := service.Authenticate(t.Context(), result.Token)
	if err != nil || authenticated.ID != user.ID {
		t.Fatalf("authenticate = %+v, %v", authenticated, err)
	}
	if err = service.Logout(t.Context(), result.Token); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(t.Context(), result.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("authenticate revoked token error = %v", err)
	}
	if err = service.Logout(t.Context(), result.Token); err != nil {
		t.Fatalf("idempotent logout: %v", err)
	}
}

func TestServiceRejectsWrongPasswordAndUnknownEmail(t *testing.T) {
	database := openAuthStore(t)
	service := NewService(database, clock.NewFixed(time.Now()), time.Hour)
	if _, err := service.Register(t.Context(), "coach@example.test", "Coach", "strong-password", domain.RoleCoach); err != nil {
		t.Fatal(err)
	}
	for _, credentials := range [][2]string{
		{"coach@example.test", "wrong-password"},
		{"missing@example.test", "wrong-password"},
	} {
		if _, err := service.Login(t.Context(), credentials[0], credentials[1]); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("Login(%q) error = %v", credentials[0], err)
		}
	}
}

func TestServiceExpiresAndPurgesSession(t *testing.T) {
	database := openAuthStore(t)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	fixed := clock.NewFixed(now)
	service := NewService(database, fixed, time.Hour)
	if _, err := service.Register(t.Context(), "guardian@example.test", "Guardian", "strong-password", domain.RoleGuardian); err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(t.Context(), "guardian@example.test", "strong-password")
	if err != nil {
		t.Fatal(err)
	}
	fixed.Advance(time.Hour)
	if _, err = service.Authenticate(t.Context(), result.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expired session error = %v", err)
	}
	count, err := service.PurgeExpired(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("PurgeExpired = %d, %v", count, err)
	}
}

func TestRegisterRejectsDuplicateEmailAndInvalidRole(t *testing.T) {
	database := openAuthStore(t)
	service := NewService(database, clock.NewFixed(time.Now()), time.Hour)
	if _, err := service.Register(t.Context(), "same@example.test", "One", "strong-password", domain.RoleStudent); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(t.Context(), "SAME@example.test", "Two", "another-password", domain.RoleGuardian); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate email error = %v", err)
	}
	if _, err := service.Register(t.Context(), "bad@example.test", "Bad", "strong-password", domain.Role("admin")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid role error = %v", err)
	}
}
