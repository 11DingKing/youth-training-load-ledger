package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/audit"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" || len(requestID) > 128 {
			var raw [12]byte
			if _, err := rand.Read(raw[:]); err != nil {
				requestID = time.Now().UTC().Format("20060102150405.000000000")
			} else {
				requestID = hex.EncodeToString(raw[:])
			}
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(audit.WithRequestID(r.Context(), requestID)))
	})
}

func Recover(logger *slog.Logger, onPanic func(http.ResponseWriter, *http.Request)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if value := recover(); value != nil {
					logger.ErrorContext(r.Context(), "request panic", "panic", value, "stack", string(debug.Stack()))
					onPanic(w, r)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func Log(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			next.ServeHTTP(w, r)
			logger.InfoContext(r.Context(), "http request", "method", r.Method, "path", r.URL.Path,
				"request_id", audit.RequestID(r.Context()), "duration_ms", time.Since(started).Milliseconds())
		})
	}
}
