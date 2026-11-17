package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mohamedsalahi/sysmon_daemon/internal/telemetry"
)

const (
	statusCodeForbidden            = "forbidden"
	statusCodeMethodNotAllowed     = "method_not_allowed"
	statusCodeTelemetryUnavailable = "telemetry_unavailable"
)

// Collector is the telemetry source used by the HTTP handler.
type Collector interface {
	Collect() (telemetry.Snapshot, error)
}

// Handler exposes health and telemetry endpoints.
type Handler struct {
	collector      Collector
	allowedOrigins map[string]struct{}
	logger         *slog.Logger
}

// Option configures a Handler.
type Option func(*Handler)

// WithAllowedOrigins sets the exact browser origins allowed to read telemetry.
// An empty list grants no cross-origin browser access.
func WithAllowedOrigins(origins []string) Option {
	return func(handler *Handler) {
		handler.allowedOrigins = make(map[string]struct{}, len(origins))
		for _, origin := range origins {
			handler.allowedOrigins[origin] = struct{}{}
		}
	}
}

// WithLogger sets the logger used for internal collection failures.
func WithLogger(logger *slog.Logger) Option {
	return func(handler *Handler) {
		if logger != nil {
			handler.logger = logger
		}
	}
}

// NewHandler creates the HTTP routes for system telemetry.
func NewHandler(collector Collector, options ...Option) http.Handler {
	handler := &Handler{
		collector:      collector,
		allowedOrigins: make(map[string]struct{}),
		logger:         slog.Default(),
	}
	for _, option := range options {
		option(handler)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sysmon", handler.handleTelemetry)
	mux.HandleFunc("/health", handler.handleHealth)
	return mux
}

func (handler *Handler) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodOptions {
		handler.handlePreflight(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, statusCodeMethodNotAllowed, "method not allowed")
		return
	}
	if !handler.authorizeOrigin(w, r) {
		writeError(w, http.StatusForbidden, statusCodeForbidden, "origin is not allowed")
		return
	}

	snapshot, err := handler.collector.Collect()
	if err != nil {
		handler.logger.Error("telemetry collection failed", "error", err)
		writeError(
			w,
			http.StatusServiceUnavailable,
			statusCodeTelemetryUnavailable,
			"system telemetry is temporarily unavailable",
		)
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
}

func (handler *Handler) handlePreflight(w http.ResponseWriter, r *http.Request) {
	if requestedMethod := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")); requestedMethod != "" && requestedMethod != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, statusCodeMethodNotAllowed, "method not allowed")
		return
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, statusCodeMethodNotAllowed, "method not allowed")
		return
	}
	if !handler.authorizeOrigin(w, r) {
		writeError(w, http.StatusForbidden, statusCodeForbidden, "origin is not allowed")
		return
	}

	w.Header().Set("Access-Control-Allow-Methods", http.MethodGet)
	w.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) authorizeOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	w.Header().Add("Vary", "Origin")
	if _, allowed := handler.allowedOrigins[origin]; !allowed {
		return false
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)
	return true
}

func (handler *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, statusCodeMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type errorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte(`{"error":{"code":"internal_server_error","message":"response encoding failed"}}`)
		status = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}
