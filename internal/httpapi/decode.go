package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return domain.FieldError{Field: "body", Problem: err.Error()}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return domain.FieldError{Field: "body", Problem: "must contain exactly one JSON value"}
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.PathValue(name))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, domain.FieldError{Field: name, Problem: "must be a positive integer"}
	}
	return id, nil
}

func requireMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, r, fmt.Errorf("%w: method not allowed", domain.ErrInvalid))
			return
		}
		next(w, r)
	}
}
