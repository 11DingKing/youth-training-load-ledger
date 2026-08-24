package httpapi

import (
	"net/http"
	"strconv"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (s *Server) listRisks(w http.ResponseWriter, r *http.Request) {
	athleteID, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	athlete, err := s.deps.Profiles.GetAuthorized(r.Context(), currentUser(r), athleteID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	_ = athlete
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	items, err := s.deps.Store.ListRisks(r.Context(), athleteID, domain.RiskStatus(r.URL.Query().Get("status")), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) acknowledgeRisk(w http.ResponseWriter, r *http.Request) {
	riskID, err := pathID(r, "riskID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.deps.Risks.Acknowledge(r.Context(), currentUser(r), riskID, input.ExpectedVersion)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) resolveRisk(w http.ResponseWriter, r *http.Request) {
	riskID, err := pathID(r, "riskID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expected_version"`
		Resolution      string `json:"resolution"`
	}
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.deps.Risks.Resolve(r.Context(), currentUser(r), riskID, input.ExpectedVersion, input.Resolution)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) resumeAthlete(w http.ResponseWriter, r *http.Request) {
	athleteID, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expected_version"`
		Basis           string `json:"basis"`
	}
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.deps.Risks.ResumeAthlete(r.Context(), currentUser(r), athleteID, input.ExpectedVersion, input.Basis)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	if actor.Role != domain.RoleAdvisor && actor.Role != domain.RoleCoach {
		writeError(w, r, domain.ErrForbidden)
		return
	}
	objectID, err := pathID(r, "objectID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	limit, offset := 50, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, _ = strconv.Atoi(raw)
	}
	items, err := s.deps.Store.ListAudit(r.Context(), r.PathValue("objectType"), objectID, limit, offset)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}
