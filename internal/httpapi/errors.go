package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/11DingKing/youth-training-load-ledger/internal/audit"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

// statusClientClosedRequest signals that the client gave up before the response
// could be written. It mirrors nginx's 499 so callers can distinguish an
// abandoned request from a real server failure.
const statusClientClosedRequest = 499

type errorBody struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	// If the caller abandoned the request, do not surface a success status.
	// WithTx rolls back on cancellation, so no committed state survives the
	// disconnect; emit a non-standard 499 instead of a 2xx.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeJSON(w, statusClientClosedRequest, errorBody{Error: APIError{
			Code: "client_closed", Message: "request was cancelled before completion",
			RequestID: audit.RequestID(r.Context()),
		}})
		return
	}
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
