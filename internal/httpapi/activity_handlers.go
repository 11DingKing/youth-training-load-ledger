package httpapi

import (
	"net/http"

	"github.com/11DingKing/youth-training-load-ledger/internal/activity"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (s *Server) recordActivity(w http.ResponseWriter, r *http.Request) {
	var input activity.RecordInput
	if err := decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.deps.Activities.Record(r.Context(), currentUser(r), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (s *Server) submitFatigue(w http.ResponseWriter, r *http.Request) {
	var input domain.FatigueReport
	if err := decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	report, riskCase, err := s.deps.Risks.SubmitFatigue(r.Context(), currentUser(r), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"fatigue_report": report, "risk_case": riskCase})
}
