package httpapi

import (
	"net/http"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/planning"
)

func (s *Server) submitScreening(w http.ResponseWriter, r *http.Request) {
	athleteID, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input planning.ScreeningInput
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.deps.Planning.SubmitScreening(r.Context(), currentUser(r), athleteID, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) reviewScreening(w http.ResponseWriter, r *http.Request) {
	athleteID, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		Clear bool   `json:"clear"`
		Basis string `json:"basis"`
	}
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.deps.Planning.ReviewScreening(r.Context(), currentUser(r), athleteID, input.Clear, input.Basis)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) recordBaseline(w http.ResponseWriter, r *http.Request) {
	athleteID, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input domain.BaselineAssessment
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	input.AthleteID = athleteID
	result, err := s.deps.Planning.RecordBaseline(r.Context(), currentUser(r), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) createPrescription(w http.ResponseWriter, r *http.Request) {
	var input planning.PrescriptionInput
	if err := decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.deps.Planning.CreatePrescription(r.Context(), currentUser(r), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) publishPrescription(w http.ResponseWriter, r *http.Request) {
	prescriptionID, err := pathID(r, "prescriptionID")
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
	result, err := s.deps.Planning.PublishPrescription(r.Context(), currentUser(r), prescriptionID, input.ExpectedVersion)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) recordReassessment(w http.ResponseWriter, r *http.Request) {
	athleteID, err := pathID(r, "athleteID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input domain.Reassessment
	if err = decodeJSON(w, r, s.deps.MaxBodyBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	input.AthleteID = athleteID
	result, err := s.deps.Planning.RecordReassessment(r.Context(), currentUser(r), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
