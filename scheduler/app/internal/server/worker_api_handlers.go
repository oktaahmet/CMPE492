package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

// registerWorkerHandler godoc
// @Summary      Register worker or heartbeat
// @Tags         workers
// @Accept       json
// @Produce      json
// @Param        body  body      RegisterWorkerRequest  true  "Worker register request"
// @Success      200   {object}  scheduler.Worker
// @Failure      400   {string}  string
// @Failure      405   {string}  string
// @Router       /api/workers/register [post]
func registerWorkerHandler(engine *scheduler.Engine, auth *workerAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req RegisterWorkerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		normalizedWorkerID, err := normalizeWorkerID(req.WorkerID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if auth != nil {
			authWorkerID, err := auth.authenticateRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			if !sameWorkerID(authWorkerID, normalizedWorkerID) {
				http.Error(w, "worker token does not match worker_id", http.StatusForbidden)
				return
			}
		}
		writeJSON(w, engine.RegisterOrHeartbeat(normalizedWorkerID), http.StatusOK)
	}
}

// pullHandler godoc
// @Summary      Pull next assignment
// @Tags         workers
// @Produce      json
// @Param        worker_id  query     string  true  "Worker ID"
// @Success      200        {object}  scheduler.Assignment
// @Success      204        {string}  string  "No assignment"
// @Failure      400        {string}  string
// @Failure      405        {string}  string
// @Router       /api/pull [get]
func pullHandler(engine *scheduler.Engine, auth *workerAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		workerID, err := normalizeWorkerID(r.URL.Query().Get("worker_id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if auth != nil {
			authWorkerID, err := auth.authenticateRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			if !sameWorkerID(authWorkerID, workerID) {
				http.Error(w, "worker token does not match worker_id", http.StatusForbidden)
				return
			}
		}
		engine.RegisterOrHeartbeat(workerID)

		assignment, ok := engine.AssignNext(workerID)
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, assignment, http.StatusOK)
	}
}

// resultHandler godoc
// @Summary      Submit job result
// @Tags         jobs
// @Accept       json
// @Produce      json
// @Param        body  body      scheduler.ResultSubmission  true  "Result payload"
// @Success      200   {object}  scheduler.Decision
// @Failure      400   {string}  string
// @Failure      405   {string}  string
// @Router       /api/result [post]
func resultHandler(
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
	maxPayloadBytes int64,
	auth *workerAuth,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxPayloadBytes)
		var req scheduler.ResultSubmission
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, "result payload too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		var err error
		req.WorkerID, err = normalizeWorkerID(req.WorkerID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if auth != nil {
			authWorkerID, err := auth.authenticateRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			if !sameWorkerID(authWorkerID, req.WorkerID) {
				http.Error(w, "worker token does not match worker_id", http.StatusForbidden)
				return
			}
		}

		decision, err := engine.SubmitResult(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if decision.Finalized {
			if err := handleBrowserFinalizedDecision(r.Context(), engine, workflowManager, store, req.JobID, decision); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		writeJSON(w, decision, http.StatusOK)
	}
}
