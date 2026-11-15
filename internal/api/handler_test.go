package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mohamedsalahi/sysmon_daemon/internal/telemetry"
)

type stubCollector struct {
	snapshot telemetry.Snapshot
	err      error
}

func (collector stubCollector) Collect() (telemetry.Snapshot, error) {
	return collector.snapshot, collector.err
}

func TestTelemetryEndpointReturnsCollectedSnapshot(t *testing.T) {
	usage := 37.5
	snapshot := telemetry.Snapshot{
		CPU: telemetry.CPUStats{
			UsagePercent: &usage,
			Status:       telemetry.CPUStatusAvailable,
		},
		Memory: telemetry.MemoryStats{
			TotalBytes:     1000,
			AvailableBytes: 400,
			UsedBytes:      600,
			CacheBytes:     200,
		},
		Processes:           12,
		SystemUptimeSeconds: 42.5,
		CollectedAt:         time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
	}
	handler := NewHandler(stubCollector{snapshot: snapshot})

	request := httptest.NewRequest(http.MethodGet, "/api/sysmon", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	if origin := response.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty by default", origin)
	}

	var decoded telemetry.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.CPU.UsagePercent == nil || *decoded.CPU.UsagePercent != usage {
		t.Fatalf("CPU usage = %v, want %f", decoded.CPU.UsagePercent, usage)
	}
	if decoded.Memory != snapshot.Memory {
		t.Fatalf("memory = %+v, want %+v", decoded.Memory, snapshot.Memory)
	}
	if decoded.Processes != snapshot.Processes {
		t.Fatalf("processes = %d, want %d", decoded.Processes, snapshot.Processes)
	}
}

func TestTelemetryEndpointUsesExactOriginAllowlist(t *testing.T) {
	handler := NewHandler(
		stubCollector{},
		WithAllowedOrigins([]string{"https://portfolio.example"}),
	)

	allowedRequest := httptest.NewRequest(http.MethodGet, "/api/sysmon", nil)
	allowedRequest.Header.Set("Origin", "https://portfolio.example")
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowedRequest)
	if allowedResponse.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, want %d", allowedResponse.Code, http.StatusOK)
	}
	if origin := allowedResponse.Header().Get("Access-Control-Allow-Origin"); origin != "https://portfolio.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q", origin)
	}
	if vary := allowedResponse.Header().Get("Vary"); vary != "Origin" {
		t.Fatalf("Vary = %q, want Origin", vary)
	}

	blockedRequest := httptest.NewRequest(http.MethodGet, "/api/sysmon", nil)
	blockedRequest.Header.Set("Origin", "https://attacker.example")
	blockedResponse := httptest.NewRecorder()
	handler.ServeHTTP(blockedResponse, blockedRequest)
	if blockedResponse.Code != http.StatusForbidden {
		t.Fatalf("blocked status = %d, want %d", blockedResponse.Code, http.StatusForbidden)
	}
	if origin := blockedResponse.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("blocked Access-Control-Allow-Origin = %q, want empty", origin)
	}
}

func TestTelemetryPreflightRequiresAllowedOrigin(t *testing.T) {
	handler := NewHandler(
		stubCollector{},
		WithAllowedOrigins([]string{"https://portfolio.example"}),
	)

	allowedRequest := httptest.NewRequest(http.MethodOptions, "/api/sysmon", nil)
	allowedRequest.Header.Set("Origin", "https://portfolio.example")
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowedRequest)
	if allowedResponse.Code != http.StatusNoContent {
		t.Fatalf("allowed preflight status = %d, want %d", allowedResponse.Code, http.StatusNoContent)
	}
	if methods := allowedResponse.Header().Get("Access-Control-Allow-Methods"); methods != http.MethodGet {
		t.Fatalf("Access-Control-Allow-Methods = %q, want GET", methods)
	}

	blockedRequest := httptest.NewRequest(http.MethodOptions, "/api/sysmon", nil)
	blockedRequest.Header.Set("Origin", "https://attacker.example")
	blockedResponse := httptest.NewRecorder()
	handler.ServeHTTP(blockedResponse, blockedRequest)
	if blockedResponse.Code != http.StatusForbidden {
		t.Fatalf("blocked preflight status = %d, want %d", blockedResponse.Code, http.StatusForbidden)
	}
}

func TestTelemetryEndpointReportsUnavailableCollector(t *testing.T) {
	handler := NewHandler(stubCollector{err: errors.New("private procfs failure detail")})

	request := httptest.NewRequest(http.MethodGet, "/api/sysmon", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(response.Body.String(), "private procfs failure detail") {
		t.Fatalf("response exposed internal error: %s", response.Body.String())
	}

	var decoded errorBody
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.Error.Code != statusCodeTelemetryUnavailable {
		t.Fatalf("error code = %q, want %q", decoded.Error.Code, statusCodeTelemetryUnavailable)
	}
}

func TestEndpointsRejectUnsupportedMethods(t *testing.T) {
	handler := NewHandler(stubCollector{})

	for _, path := range []string{"/api/sysmon", "/health"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusMethodNotAllowed)
		}
		if allow := response.Header().Get("Allow"); allow != http.MethodGet {
			t.Fatalf("%s Allow = %q, want GET", path, allow)
		}
	}
}

func TestHealthEndpointReturnsOK(t *testing.T) {
	handler := NewHandler(stubCollector{})

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != `{"status":"ok"}` {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestParseAllowedOrigins(t *testing.T) {
	origins, err := ParseAllowedOrigins("https://portfolio.example, http://localhost:3000")
	if err != nil {
		t.Fatalf("ParseAllowedOrigins() error = %v", err)
	}
	if len(origins) != 2 {
		t.Fatalf("origins = %#v, want two values", origins)
	}
	if origins[0] != "https://portfolio.example" || origins[1] != "http://localhost:3000" {
		t.Fatalf("origins = %#v", origins)
	}

	empty, err := ParseAllowedOrigins("")
	if err != nil {
		t.Fatalf("ParseAllowedOrigins(empty) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty origins = %#v", empty)
	}
}

func TestParseAllowedOriginsRejectsUnsafeValues(t *testing.T) {
	for _, raw := range []string{
		"*",
		"https://*.example.com",
		"https://user@example.com",
		"https://example.com/path",
		"https://example.com?query=1",
		"https://example.com#fragment",
		"ftp://example.com",
		"https://example.com,,https://other.example",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseAllowedOrigins(raw); err == nil {
				t.Fatalf("ParseAllowedOrigins(%q) error = nil", raw)
			}
		})
	}
}
