package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	activityservice "github.com/11DingKing/youth-training-load-ledger/internal/activity"
	"github.com/11DingKing/youth-training-load-ledger/internal/auth"
	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	planningservice "github.com/11DingKing/youth-training-load-ledger/internal/planning"
	profileservice "github.com/11DingKing/youth-training-load-ledger/internal/profile"
	riskservice "github.com/11DingKing/youth-training-load-ledger/internal/risk"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
)

type apiFixture struct {
	t       *testing.T
	handler http.Handler
	store   *store.Store
	auth    *auth.Service
	now     *clock.Fixed
	advisor domain.User
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	now := clock.NewFixed(time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "http.db"))
	if err != nil {
		t.Fatal(err)
	}
	authService := auth.NewService(database, now, time.Hour)
	advisor, err := authService.Register(t.Context(), "advisor@http.test", "HTTP Advisor", "strong-password", domain.RoleAdvisor)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := New(Dependencies{
		Store: database, Auth: authService,
		Profiles:   profileservice.NewService(database, now),
		Planning:   planningservice.NewService(database, now),
		Activities: activityservice.NewService(database, now),
		Risks:      riskservice.NewService(database, now),
		Logger:     logger, MaxBodyBytes: 4096,
	})
	fixture := &apiFixture{t: t, handler: api.Handler(), store: database, auth: authService, now: now, advisor: advisor}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return fixture
}

func (f *apiFixture) request(method, path, token, requestID string, body any) (*http.Response, []byte) {
	f.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(f.t.Context(), method, path, reader)
	if err != nil {
		f.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, req)
	response := recorder.Result()
	payload, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		f.t.Fatal(err)
	}
	return response, payload
}

func (f *apiFixture) login() string {
	f.t.Helper()
	response, payload := f.request(http.MethodPost, "/v1/auth/login", "", "login-request", map[string]any{
		"email": "advisor@http.test", "password": "strong-password",
	})
	if response.StatusCode != http.StatusOK {
		f.t.Fatalf("login status=%d body=%s", response.StatusCode, payload)
	}
	var result struct {
		Token string      `json:"token"`
		User  domain.User `json:"user"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		f.t.Fatal(err)
	}
	if result.Token == "" || result.User.ID != f.advisor.ID {
		f.t.Fatalf("login body = %s", payload)
	}
	return result.Token
}

func TestHealthAndReadinessExposeDependencyState(t *testing.T) {
	fixture := newAPIFixture(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		response, payload := fixture.request(http.MethodGet, path, "", "probe-1", nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.StatusCode, payload)
		}
		if response.Header.Get("X-Request-ID") != "probe-1" {
			t.Fatalf("GET %s request id = %q", path, response.Header.Get("X-Request-ID"))
		}
		if !bytes.Contains(payload, []byte(`"status"`)) {
			t.Fatalf("GET %s payload=%s", path, payload)
		}
	}
}

func TestAuthenticationLifecycleThroughHTTP(t *testing.T) {
	fixture := newAPIFixture(t)
	response, payload := fixture.request(http.MethodGet, "/v1/me", "", "unauthorized-1", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized me status=%d body=%s", response.StatusCode, payload)
	}
	var denied errorBody
	if err := json.Unmarshal(payload, &denied); err != nil {
		t.Fatal(err)
	}
	if denied.Error.Code != "unauthorized" || denied.Error.RequestID != "unauthorized-1" {
		t.Fatalf("unauthorized error = %+v", denied)
	}
	token := fixture.login()
	response, payload = fixture.request(http.MethodGet, "/v1/me", token, "me-1", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized me status=%d body=%s", response.StatusCode, payload)
	}
	var me domain.User
	if err := json.Unmarshal(payload, &me); err != nil {
		t.Fatal(err)
	}
	if me.ID != fixture.advisor.ID || me.PasswordHash != "" {
		t.Fatalf("me = %+v", me)
	}
	response, payload = fixture.request(http.MethodPost, "/v1/auth/logout", token, "logout-1", nil)
	if response.StatusCode != http.StatusNoContent || len(payload) != 0 {
		t.Fatalf("logout status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = fixture.request(http.MethodGet, "/v1/me", token, "after-logout", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d body=%s", response.StatusCode, payload)
	}
}

func TestLoginRejectsBadCredentialsWithoutLeakingIdentity(t *testing.T) {
	fixture := newAPIFixture(t)
	for _, input := range []map[string]any{
		{"email": "advisor@http.test", "password": "wrong-password"},
		{"email": "missing@http.test", "password": "wrong-password"},
	} {
		response, payload := fixture.request(http.MethodPost, "/v1/auth/login", "", "bad-login", input)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("login status=%d body=%s", response.StatusCode, payload)
		}
		var result errorBody
		if err := json.Unmarshal(payload, &result); err != nil {
			t.Fatal(err)
		}
		if result.Error.Code != "unauthorized" || result.Error.Message != "authentication required or expired" {
			t.Fatalf("credential error differs: %+v", result)
		}
	}
}

func TestStrictJSONParsingRejectsUnknownAndTrailingValues(t *testing.T) {
	fixture := newAPIFixture(t)
	tests := []string{
		`{"email":"advisor@http.test","password":"strong-password","admin":true}`,
		`{"email":"advisor@http.test","password":"strong-password"}{}`,
		`{"email":`,
	}
	for _, raw := range tests {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/login", strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		fixture.handler.ServeHTTP(recorder, req)
		response := recorder.Result()
		payload, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("raw=%q status=%d body=%s", raw, response.StatusCode, payload)
		}
	}
}

func TestExpiredBearerTokenIsRejected(t *testing.T) {
	fixture := newAPIFixture(t)
	token := fixture.login()
	fixture.now.Advance(time.Hour)
	response, payload := fixture.request(http.MethodGet, "/v1/me", token, "expired-1", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired status=%d body=%s", response.StatusCode, payload)
	}
	var result errorBody
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != "unauthorized" || result.Error.RequestID != "expired-1" {
		t.Fatalf("expired error = %+v", result)
	}
}

func TestGeneratedRequestIDIsPresent(t *testing.T) {
	fixture := newAPIFixture(t)
	response, _ := fixture.request(http.MethodGet, "/healthz", "", "", nil)
	requestID := response.Header.Get("X-Request-ID")
	if len(requestID) < 16 || len(requestID) > 128 {
		t.Fatalf("generated request id = %q", requestID)
	}
}

func TestAuditEndpointRequiresProfessionalRole(t *testing.T) {
	fixture := newAPIFixture(t)
	student, err := fixture.auth.Register(t.Context(), "student@http.test", "HTTP Student", "strong-password", domain.RoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	_ = student
	login, err := fixture.auth.Login(t.Context(), "student@http.test", "strong-password")
	if err != nil {
		t.Fatal(err)
	}
	response, payload := fixture.request(http.MethodGet, "/v1/audit/athlete/1", login.Token, "audit-denied", nil)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("student audit status=%d body=%s", response.StatusCode, payload)
	}
}

func TestCancelledWriteRequestDoesNotReturnSuccessStatus(t *testing.T) {
	fixture := newAPIFixture(t)

	// Submit a screening against an already-cancelled request context so that
	// WithTx returns context.Canceled before any commit. The handler must not
	// surface a 2xx; writeError maps the cancellation to 499 client_closed.
	athlete, err := fixture.auth.Register(t.Context(), "cancel-student@http.test", "Cancel Student", "strong-password", domain.RoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	guardian, err := fixture.auth.Register(t.Context(), "cancel-guardian@http.test", "Cancel Guardian", "strong-password", domain.RoleGuardian)
	if err != nil {
		t.Fatal(err)
	}
	advisor := fixture.advisor
	var athleteRow domain.Athlete
	if err = fixture.store.WithTx(t.Context(), func(tx *store.Tx) error {
		athleteRow, err = tx.CreateAthlete(t.Context(), domain.Athlete{
			StudentUserID: athlete.ID, GuardianUserID: guardian.ID, AdvisorUserID: &advisor.ID,
			BirthDate: fixture.now.Now().AddDate(-14, 0, 0), Timezone: "UTC", Status: domain.AthleteActive,
			Version: 1, CreatedAt: fixture.now.Now(), UpdatedAt: fixture.now.Now(),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	studentLogin, err := fixture.auth.Login(t.Context(), "cancel-student@http.test", "strong-password")
	if err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"answers": map[string]any{"q1": "no"}, "risk_flag": false}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("/v1/athletes/%d/screenings", athleteRow.ID), bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+studentLogin.Token)
	req.Header.Set("X-Request-ID", "cancel-1")

	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, req)
	response := recorder.Result()
	payload, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode < 400 {
		t.Fatalf("cancelled write returned success status=%d body=%s", response.StatusCode, payload)
	}
	var result errorBody
	if err = json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("decode error body: %v (payload=%s)", err, payload)
	}
	if result.Error.Code != "client_closed" || result.Error.RequestID != "cancel-1" {
		t.Fatalf("cancelled write error = %+v", result)
	}
}
