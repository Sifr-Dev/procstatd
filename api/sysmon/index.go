// Package handler exposes the JSON HTTP surface for the telemetry daemon.
// The handler is pure: wire it to a *internal.Telemetry from main.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/mohamedsalahi/sysmon_daemon/internal"
)

func Sysmon(t *internal.Telemetry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		stats, err := t.Collect()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(stats)
	}
}
