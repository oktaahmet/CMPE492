package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"x402-scheduler/internal/storage/postgres"
)

// healthHandler is a cheap liveness check; it does not touch external systems.
func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, HealthResponse{
			Status:    "ok",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Checks: map[string]string{
				"http": "ok",
			},
		}, http.StatusOK)
	}
}

// readinessHandler verifies dependencies that must be available before serving
// real traffic.
func readinessHandler(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		resp := HealthResponse{
			Status:    "ready",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Checks: map[string]string{
				"http": "ok",
				"db":   "ok",
			},
		}

		if err := store.Ping(ctx); err != nil {
			resp.Status = "not_ready"
			resp.Checks["db"] = fmt.Sprintf("error: %v", err)
			writeJSON(w, resp, http.StatusServiceUnavailable)
			return
		}

		writeJSON(w, resp, http.StatusOK)
	}
}

// writeJSON centralizes response encoding so handlers use the same content type
// and status-code ordering.
func writeJSON(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
