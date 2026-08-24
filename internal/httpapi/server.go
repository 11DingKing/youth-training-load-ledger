package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/activity"
	"github.com/11DingKing/youth-training-load-ledger/internal/auth"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/middleware"
	"github.com/11DingKing/youth-training-load-ledger/internal/planning"
	"github.com/11DingKing/youth-training-load-ledger/internal/profile"
	"github.com/11DingKing/youth-training-load-ledger/internal/risk"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
)

type Dependencies struct {
	Store        *store.Store
	Auth         *auth.Service
	Profiles     *profile.Service
	Planning     *planning.Service
	Activities   *activity.Service
	Risks        *risk.Service
	Logger       *slog.Logger
	MaxBodyBytes int64
}

type Server struct {
	deps Dependencies
	mux  *http.ServeMux
}

func New(deps Dependencies) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	server := &Server{deps: deps, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	var handler http.Handler = s.mux
	handler = middleware.Log(s.deps.Logger)(handler)
	handler = middleware.Recover(s.deps.Logger, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.ErrAbortHandler)
	})(handler)
	handler = middleware.RequestID(handler)
	return handler
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /readyz", s.ready)
	s.mux.HandleFunc("POST /v1/auth/login", s.login)
	s.mux.Handle("POST /v1/auth/logout", s.requireAuth(http.HandlerFunc(s.logout)))
	s.mux.Handle("GET /v1/me", s.requireAuth(http.HandlerFunc(s.me)))
	s.mux.Handle("POST /v1/athletes", s.requireAuth(http.HandlerFunc(s.createAthlete)))
	s.mux.Handle("GET /v1/athletes", s.requireAuth(http.HandlerFunc(s.listAthletes)))
	s.mux.Handle("GET /v1/athletes/{athleteID}", s.requireAuth(http.HandlerFunc(s.getAthlete)))
	s.mux.Handle("POST /v1/athletes/{athleteID}/consent", s.requireAuth(http.HandlerFunc(s.grantConsent)))
	s.mux.Handle("POST /v1/athletes/{athleteID}/consent/withdraw", s.requireAuth(http.HandlerFunc(s.withdrawConsent)))
	s.mux.Handle("POST /v1/athletes/{athleteID}/screenings", s.requireAuth(http.HandlerFunc(s.submitScreening)))
	s.mux.Handle("POST /v1/athletes/{athleteID}/screenings/review", s.requireAuth(http.HandlerFunc(s.reviewScreening)))
	s.mux.Handle("POST /v1/athletes/{athleteID}/baselines", s.requireAuth(http.HandlerFunc(s.recordBaseline)))
	s.mux.Handle("POST /v1/athletes/{athleteID}/activate", s.requireAuth(http.HandlerFunc(s.activateAthlete)))
	s.mux.Handle("POST /v1/prescriptions", s.requireAuth(http.HandlerFunc(s.createPrescription)))
	s.mux.Handle("POST /v1/prescriptions/{prescriptionID}/publish", s.requireAuth(http.HandlerFunc(s.publishPrescription)))
	s.mux.Handle("POST /v1/activities", s.requireAuth(http.HandlerFunc(s.recordActivity)))
	s.mux.Handle("POST /v1/fatigue", s.requireAuth(http.HandlerFunc(s.submitFatigue)))
	s.mux.Handle("GET /v1/athletes/{athleteID}/risks", s.requireAuth(http.HandlerFunc(s.listRisks)))
	s.mux.Handle("POST /v1/risks/{riskID}/acknowledge", s.requireAuth(http.HandlerFunc(s.acknowledgeRisk)))
	s.mux.Handle("POST /v1/risks/{riskID}/resolve", s.requireAuth(http.HandlerFunc(s.resolveRisk)))
	s.mux.Handle("POST /v1/athletes/{athleteID}/reassessments", s.requireAuth(http.HandlerFunc(s.recordReassessment)))
	s.mux.Handle("POST /v1/athletes/{athleteID}/resume", s.requireAuth(http.HandlerFunc(s.resumeAthlete)))
	s.mux.Handle("GET /v1/audit/{objectType}/{objectID}", s.requireAuth(http.HandlerFunc(s.listAudit)))
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, r, domain.ErrUnauthorized)
			return
		}
		token := strings.TrimSpace(parts[1])
		user, err := s.deps.Auth.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, r, err)
			return
		}
		ctx := middleware.WithToken(middleware.WithUser(r.Context(), user), token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentUser(r *http.Request) domain.User {
	user, _ := middleware.User(r.Context())
	return user
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()
	if err := s.deps.Store.Ready(ctx); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func contextWithTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), timeout)
}
