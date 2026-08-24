package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/11DingKing/youth-training-load-ledger/internal/audit"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

type errorBody struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	switch {
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrExpired), errors.Is(err, domain.ErrRevoked):
		status, code, message = http.StatusUnauthorized, "unauthorized", "authentication required or expired"
	case errors.Is(err, domain.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "operation is not permitted"
	case errors.Is(err, domain.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "resource was not found"
	case errors.Is(err, domain.ErrVersionConflict):
		status, code, message = http.StatusConflict, "version_conflict", "resource changed; reload and retry"
	case errors.Is(err, domain.ErrConflict):
		status, code, message = http.StatusConflict, "conflict", "request conflicts with existing state"
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrInvalidState),
		errors.Is(err, domain.ErrConsentRequired), errors.Is(err, domain.ErrBaselineRequired),
		errors.Is(err, domain.ErrRiskOpen), errors.Is(err, domain.ErrTrainingPaused),
		errors.Is(err, domain.ErrLoadExceeded):
		status, code, message = http.StatusUnprocessableEntity, "business_rule", err.Error()
	}
	writeJSON(w, status, errorBody{Error: APIError{Code: code, Message: message, RequestID: audit.RequestID(r.Context())}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
