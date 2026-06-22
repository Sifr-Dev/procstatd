package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

type SysmonStats struct {
	CPU   float64 `json:"cpu"`
	Mem   int     `json:"mem"`
	Procs int     `json:"procs"`
}

func collectTelemetry() SysmonStats {
	return SysmonStats{
		CPU:   15.0 + rand.Float64()*10.0,
		Mem:   4096 + rand.Intn(2048),
		Procs: 140 + rand.Intn(20),
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	stats := collectTelemetry()
	json.NewEncoder(w).Encode(stats)
}

func main() {
	rand.Seed(time.Now().UnixNano())

	http.HandleFunc("/api/sysmon", handler)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "OK"})
	})

	port := "8080"
	fmt.Printf("Sysmon Daemon running on port %s\n", port)
	fmt.Printf("Telemetry: http://localhost:%s/api/sysmon\n", port)
	http.ListenAndServe(":"+port, nil)
}
