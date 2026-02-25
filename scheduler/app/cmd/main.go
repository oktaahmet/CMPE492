package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	_ "x402-scheduler/docs"
	"x402-scheduler/internal/scheduler"
)

type RegisterWorkerRequest struct {
	WorkerID string `json:"worker_id"`
}

type ProcessPaymentsResponse struct {
	ProcessedCount int `json:"processed_count"`
}

// @title           x402 Scheduler API
// @version         1.0
// @description     Worker registration, job pull, result submit, payments, stats.
// @host            localhost:8080
// @BasePath        /
func main() {
	engine := scheduler.NewEngine(scheduler.Config{
		ReplicationFactor: loadReplicationFactor(),
		AssignmentTTL:     30 * time.Second,
	})
	workflowManager := scheduler.NewWorkflowManager()
	engine.SetPaymentProvider(loadPaymentProvider())

	if err := bootstrapWorkflow(engine, workflowManager); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/workers/register", registerWorkerHandler(engine))
	http.HandleFunc("/api/pull", pullHandler(engine))
	http.HandleFunc("/api/result", resultHandler(engine, workflowManager))
	http.HandleFunc("/api/payments", paymentsHandler(engine))
	http.HandleFunc("/api/payments/process", processPaymentsHandler(engine))
	http.HandleFunc("/api/stats", statsHandler(engine))
	http.Handle("/api/docs/", httpSwagger.WrapHandler)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	log.Println("Server started at port 8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}

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
func registerWorkerHandler(engine *scheduler.Engine) http.HandlerFunc {
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
		if req.WorkerID == "" {
			http.Error(w, "worker_id is required", http.StatusBadRequest)
			return
		}
		writeJSON(w, engine.RegisterOrHeartbeat(req.WorkerID), http.StatusOK)
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
func pullHandler(engine *scheduler.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		workerID := r.URL.Query().Get("worker_id")
		if workerID == "" {
			http.Error(w, "worker_id is required", http.StatusBadRequest)
			return
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
func resultHandler(engine *scheduler.Engine, workflowManager *scheduler.WorkflowManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req scheduler.ResultSubmission
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		decision, err := engine.SubmitResult(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if decision.Finalized {
			output, _ := engine.FinalizedOutput(req.JobID)
			nextJobs, err := workflowManager.OnJobFinalized(req.JobID, output)
			if err != nil {
				log.Printf("workflow progression failed for %s: %v", req.JobID, err)
			} else {
				for _, job := range nextJobs {
					if err := engine.Enqueue(job); err != nil {
						log.Printf("enqueue unlocked workflow node failed: job=%s err=%v", job.ID, err)
					}
				}
			}
		}
		writeJSON(w, decision, http.StatusOK)
	}
}

// paymentsHandler godoc
// @Summary      List payment queue
// @Tags         payments
// @Produce      json
// @Success      200  {array}   scheduler.PaymentEvent
// @Failure      405  {string}  string
// @Router       /api/payments [get]
func paymentsHandler(engine *scheduler.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, engine.PaymentQueueSnapshot(), http.StatusOK)
	}
}

// processPaymentsHandler godoc
// @Summary      Process pending payments
// @Tags         payments
// @Produce      json
// @Success      200  {object}  ProcessPaymentsResponse
// @Failure      405  {string}  string
// @Router       /api/payments/process [post]
func processPaymentsHandler(engine *scheduler.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, ProcessPaymentsResponse{
			ProcessedCount: engine.ProcessPayments(),
		}, http.StatusOK)
	}
}

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
		writeJSON(w, engine.StatsSnapshot(), http.StatusOK)
	}
}

func writeJSON(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func bootstrapWorkflow(engine *scheduler.Engine, workflowManager *scheduler.WorkflowManager) error {
	path := strings.TrimSpace(os.Getenv("WORKFLOW_BOOT_FILE"))
	if path == "" {
		path = filepath.Join("workflows", "prime-example", "prime-example.json")
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("workflow bootstrap open failed (%s): %w", path, err)
	}

	defer func() {
		if err := file.Close(); err != nil {
			log.Println("close error:", err)
		}
	}()

	var spec scheduler.WorkflowSpec
	if err := json.NewDecoder(file).Decode(&spec); err != nil {
		return fmt.Errorf("workflow bootstrap decode failed (%s): %w", path, err)
	}

	result, jobs, err := workflowManager.LoadWorkflow(spec)
	if err != nil {
		return fmt.Errorf("workflow bootstrap load failed (%s): %w", path, err)
	}
	if err := enqueueWorkflowJobs(engine, jobs); err != nil {
		workflowManager.DeleteWorkflow(result.WorkflowID)
		return fmt.Errorf("workflow bootstrap enqueue failed (%s): %w", path, err)
	}

	log.Printf(
		"workflow bootstrapped: file=%s workflow_id=%s topo_nodes=%d initial_jobs=%d",
		path,
		result.WorkflowID,
		len(result.TopologicalOrder),
		len(result.EnqueuedJobIDs),
	)
	return nil
}

func enqueueWorkflowJobs(engine *scheduler.Engine, jobs []scheduler.Job) error {
	for _, job := range jobs {
		if err := engine.Enqueue(job); err != nil {
			return err
		}
	}
	return nil
}
