// sysmon_daemon — Linux /proc telemetry over HTTP.
// REAL data only; on read failure the API returns 503 with the reason.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	handler "github.com/mohamedsalahi/sysmon_daemon/api/sysmon"
	"github.com/mohamedsalahi/sysmon_daemon/internal"
)

func main() {
	t := internal.NewTelemetry()

	mux := http.NewServeMux()
	mux.Handle("/api/sysmon", handler.Sysmon(t))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("sysmon_daemon listening on :%s\n", port)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
