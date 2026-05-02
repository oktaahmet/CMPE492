package server

import (
	"net/http"

	"x402-scheduler/internal/scheduler"
)

// statsHandler godoc
// @Summary      Scheduler stats snapshot
// @Tags         stats
// @Produce      json
// @Success      200  {object}  scheduler.Stats
// @Failure      405  {string}  string
// @Router       /api/stats [get]
func statsHandler(engine *scheduler.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// StatsSnapshot is already lock-protected inside the engine, so the
		// handler can return it directly without stitching together state here.
		writeJSON(w, engine.StatsSnapshot(), http.StatusOK)
	}
}
