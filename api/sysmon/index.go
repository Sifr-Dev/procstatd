package handler

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"time"
)

type SysmonStats struct {
	CPU   float64 `json:"cpu"`
	Mem   int     `json:"mem"`
	Procs int     `json:"procs"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Simulate reading from a Linux /proc filesystem via C daemon
	rand.Seed(time.Now().UnixNano())

	stats := SysmonStats{
		CPU:   15.0 + rand.Float64()*10.0,
		Mem:   4096 + rand.Intn(2048),
		Procs: 140 + rand.Intn(20),
	}

	json.NewEncoder(w).Encode(stats)
}
