package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/profile"
)

func (s *Server) createAthlete(w http.ResponseWriter, r *http.Request) {
	var input profile.CreateAthleteInput
	if err := decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	athlete, err := s.deps.Profiles.CreateAthlete(r.Context(), currentUser(r), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, athlete)
}

func (s *Server) listAthletes(w http.ResponseWriter, r *http.Request) {
	limit, offset := 25, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, _ = strconv.Atoi(raw)
	}
	items, err := s.deps.Store.ListAthletes(r.Context(), currentUser(r), limit, offset)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func (s *Server) getAthlete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	athlete, err := s.deps.Profiles.GetAuthorized(r.Context(), currentUser(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, athlete)
}

func (s *Server) grantConsent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	athlete, err := s.deps.Profiles.GrantConsent(r.Context(), currentUser(r), id, input.ExpiresAt)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, athlete)
}

func (s *Server) withdrawConsent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	athlete, err := s.deps.Profiles.WithdrawConsent(r.Context(), currentUser(r), id, input.Reason)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, athlete)
}

func (s *Server) activateAthlete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	athlete, err := s.deps.Profiles.Activate(r.Context(), currentUser(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, athlete)
}
