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

	"x402-scheduler/internal/scheduler"
)

func main() {
	loadEnvFile()

	engine := scheduler.NewEngine(scheduler.Config{
		ReplicationFactor: loadReplicationFactor(),
		AssignmentTTL:     30 * time.Second,
	})
	workflowManager := scheduler.NewWorkflowManager()
	engine.SetPaymentProvider(loadPaymentProvider())

	if err := bootstrapWorkflow(engine, workflowManager); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/workers/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			WorkerID string `json:"worker_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		if req.WorkerID == "" {
			http.Error(w, "worker_id is required", http.StatusBadRequest)
			return
		}
		writeJSON(w, engine.RegisterOrHeartbeat(req.WorkerID), http.StatusOK)
	})

	http.HandleFunc("/api/pull", func(w http.ResponseWriter, r *http.Request) {
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
	})

	http.HandleFunc("/api/result", func(w http.ResponseWriter, r *http.Request) {
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
	})

	http.HandleFunc("/api/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, engine.PaymentQueueSnapshot(), http.StatusOK)
	})

	http.HandleFunc("/api/payments/process", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{
			"processed_count": engine.ProcessPayments(),
		}, http.StatusOK)
	})

	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, engine.StatsSnapshot(), http.StatusOK)
	})

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	log.Println("Server started at port 8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
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
