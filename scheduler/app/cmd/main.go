package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	_ "x402-scheduler/docs"
	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

type RegisterWorkerRequest struct {
	WorkerID string `json:"worker_id"`
}

type ProcessPaymentsResponse struct {
	ProcessedCount int `json:"processed_count"`
}

type NodeOutputChunkResponse struct {
	Mode       string `json:"mode"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	NextOffset int    `json:"next_offset,omitempty"`
	Done       bool   `json:"done"`

	TotalItems int    `json:"total_items,omitempty"`
	TotalChars int    `json:"total_chars,omitempty"`
	Items      []any  `json:"items,omitempty"`
	Data       string `json:"data,omitempty"`
}

// @title           x402 Scheduler API
// @version         1.0
// @description     Worker registration, job pull, result submit, payments, stats.
// @host            localhost:8080
// @BasePath        /
func main() {
	replicationFactor := loadReplicationFactor()
	maxResultPayloadBytes := loadMaxResultPayloadBytes()
	engine := scheduler.NewEngine(scheduler.Config{
		ReplicationFactor: replicationFactor,
		AssignmentTTL:     30 * time.Second,
	})
	workflowManager := scheduler.NewWorkflowManager()
	engine.SetPaymentProvider(loadPaymentProvider())

	store, err := postgres.NewStore(loadPostgresDSN())
	if err != nil {
		log.Fatalf("failed to connect postgres: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			log.Printf("failed to close postgres connection: %v", closeErr)
		}
	}()
	if err := store.Migrate(context.Background()); err != nil {
		log.Fatalf("failed to run postgres migrations: %v", err)
	}
	pendingPayments, err := store.ListPendingPaymentEvents(context.Background())
	if err != nil {
		log.Fatalf("failed to load pending payments: %v", err)
	}
	engine.RestorePendingPayments(pendingPayments)
	stopPaymentsProcessor := startPaymentsProcessor(engine, store, loadPaymentsProcessInterval())
	defer stopPaymentsProcessor()
	stopAssignmentJanitor := startAssignmentJanitor(engine, 5*time.Second)
	defer stopAssignmentJanitor()

	if err := bootstrapWorkflow(engine, workflowManager, store); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/workers/register", registerWorkerHandler(engine))
	http.HandleFunc("/api/pull", pullHandler(engine))
	http.HandleFunc("/api/result", resultHandler(engine, workflowManager, store, maxResultPayloadBytes))
	http.HandleFunc("/api/workflow/node-output", workflowNodeOutputHandler(store))
	http.HandleFunc("/api/workflow/node-output/chunk", workflowNodeOutputChunkHandler(store))
	http.HandleFunc("/api/payments", paymentsHandler(store))
	http.HandleFunc("/api/payments/process", processPaymentsHandler(engine, store))
	http.HandleFunc("/api/stats", statsHandler(engine))
	http.HandleFunc("/api/runtime", adminRuntimeHandler(engine, workflowManager, store))
	adminToken := loadAdminAPIToken()
	if adminToken != "" {
		http.HandleFunc(
			"/api/admin/workflows",
			withAdminToken(adminToken, adminWorkflowListHandler(workflowManager, store)),
		)
		http.HandleFunc(
			"/api/admin/workflows/upload",
			withAdminToken(adminToken, adminWorkflowUploadHandler(workflowManager)),
		)
		http.HandleFunc(
			"/api/admin/workflows/activate",
			withAdminToken(adminToken, adminWorkflowActivateHandler(engine, workflowManager, store)),
		)
		http.HandleFunc(
			"/api/admin/workflows/delete",
			withAdminToken(adminToken, adminWorkflowDeleteHandler(engine, workflowManager, store)),
		)
		http.HandleFunc(
			"/api/admin/runtime",
			withAdminToken(adminToken, adminRuntimeHandler(engine, workflowManager, store)),
		)
	} else {
		log.Println("ADMIN_API_TOKEN not set: admin endpoints disabled")
	}
	http.Handle("/api/docs/", httpSwagger.WrapHandler)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	log.Println("Server started at port 8080")
	err = http.ListenAndServe(":8080", nil)
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
func resultHandler(
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
	maxPayloadBytes int64,
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

		decision, err := engine.SubmitResult(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if decision.Finalized {
			output, ok := engine.FinalizedOutput(req.JobID)
			if !ok {
				output = map[string]any{}
			}
			workflowID, nodeID, found := engine.JobIdentity(req.JobID)
			if found {
				if err := store.UpsertWorkflowNodeCompletion(
					r.Context(),
					workflowID,
					nodeID,
					req.JobID,
					decision.AcceptedResult,
					output,
					time.Now().UTC(),
				); err != nil {
					http.Error(w, "failed to persist finalized workflow node state", http.StatusInternalServerError)
					return
				}
			} else {
				http.Error(w, "job identity not found for finalized result", http.StatusInternalServerError)
				return
			}
			if err := store.UpsertPaymentEvents(r.Context(), engine.PaymentQueueSnapshot()); err != nil {
				http.Error(w, "failed to persist payment queue", http.StatusInternalServerError)
				return
			}

			nextJobs, err := workflowManager.OnJobFinalized(req.JobID, output)
			if err != nil {
				http.Error(w, "failed to progress workflow", http.StatusInternalServerError)
				return
			} else {
				if err := enqueueWorkflowJobs(engine, nextJobs); err != nil {
					http.Error(w, "failed to enqueue unlocked workflow jobs", http.StatusInternalServerError)
					return
				}
			}
			triggerPaymentProcessingAsync(engine, store)
		}
		writeJSON(w, decision, http.StatusOK)
	}
}

// workflowNodeOutputHandler godoc
// @Summary      Get finalized workflow node output
// @Tags         workflow
// @Produce      json
// @Param        workflow_id  query     string  true  "Workflow ID"
// @Param        node_id      query     string  true  "Node ID"
// @Success      200          {object}  map[string]any
// @Failure      400          {string}  string
// @Failure      404          {string}  string
// @Failure      405          {string}  string
// @Router       /api/workflow/node-output [get]
func workflowNodeOutputHandler(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id"))
		nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
		if workflowID == "" || nodeID == "" {
			http.Error(w, "workflow_id and node_id are required", http.StatusBadRequest)
			return
		}

		output, found, err := store.LoadWorkflowNodeOutput(r.Context(), workflowID, nodeID)
		if err != nil {
			http.Error(w, "failed to load workflow node output", http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "workflow node output not found", http.StatusNotFound)
			return
		}

		writeJSON(w, output, http.StatusOK)
	}
}

// workflowNodeOutputChunkHandler godoc
// @Summary      Get chunk of finalized workflow node output
// @Tags         workflow
// @Produce      json
// @Param        workflow_id  query     string  true   "Workflow ID"
// @Param        node_id      query     string  true   "Node ID"
// @Param        offset       query     int     false  "Chunk offset"
// @Param        limit        query     int     false  "Chunk size"
// @Success      200          {object}  NodeOutputChunkResponse
// @Failure      400          {string}  string
// @Failure      404          {string}  string
// @Failure      405          {string}  string
// @Router       /api/workflow/node-output/chunk [get]
func workflowNodeOutputChunkHandler(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id"))
		nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
		if workflowID == "" || nodeID == "" {
			http.Error(w, "workflow_id and node_id are required", http.StatusBadRequest)
			return
		}

		offset := parsePositiveIntQuery(r.URL.Query().Get("offset"), 0)
		limit := parsePositiveIntQuery(r.URL.Query().Get("limit"), 256)
		if limit > 2000 {
			limit = 2000
		}

		output, found, err := store.LoadWorkflowNodeOutput(r.Context(), workflowID, nodeID)
		if err != nil {
			http.Error(w, "failed to load workflow node output", http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "workflow node output not found", http.StatusNotFound)
			return
		}

		value, hasValue := output["output"]
		if !hasValue {
			writeJSON(w, NodeOutputChunkResponse{
				Mode:   "missing",
				Offset: offset,
				Limit:  limit,
				Done:   true,
			}, http.StatusOK)
			return
		}

		switch typed := value.(type) {
		case []any:
			if offset > len(typed) {
				offset = len(typed)
			}
			end := offset + limit
			if end > len(typed) {
				end = len(typed)
			}
			nextOffset := 0
			done := end >= len(typed)
			if !done {
				nextOffset = end
			}
			writeJSON(w, NodeOutputChunkResponse{
				Mode:       "array",
				Offset:     offset,
				Limit:      limit,
				NextOffset: nextOffset,
				Done:       done,
				TotalItems: len(typed),
				Items:      typed[offset:end],
			}, http.StatusOK)
			return
		case string:
			runes := []rune(typed)
			if offset > len(runes) {
				offset = len(runes)
			}
			end := offset + limit
			if end > len(runes) {
				end = len(runes)
			}
			nextOffset := 0
			done := end >= len(runes)
			if !done {
				nextOffset = end
			}
			writeJSON(w, NodeOutputChunkResponse{
				Mode:       "string",
				Offset:     offset,
				Limit:      limit,
				NextOffset: nextOffset,
				Done:       done,
				TotalChars: len(runes),
				Data:       string(runes[offset:end]),
			}, http.StatusOK)
			return
		default:
			raw, marshalErr := json.Marshal(typed)
			if marshalErr != nil {
				http.Error(w, "failed to encode output", http.StatusInternalServerError)
				return
			}
			runes := []rune(string(raw))
			if offset > len(runes) {
				offset = len(runes)
			}
			end := offset + limit
			if end > len(runes) {
				end = len(runes)
			}
			nextOffset := 0
			done := end >= len(runes)
			if !done {
				nextOffset = end
			}
			writeJSON(w, NodeOutputChunkResponse{
				Mode:       "json",
				Offset:     offset,
				Limit:      limit,
				NextOffset: nextOffset,
				Done:       done,
				TotalChars: len(runes),
				Data:       string(runes[offset:end]),
			}, http.StatusOK)
		}
	}
}

func parsePositiveIntQuery(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// paymentsHandler godoc
// @Summary      List payment queue
// @Tags         payments
// @Produce      json
// @Success      200  {array}   scheduler.PaymentEvent
// @Failure      405  {string}  string
// @Router       /api/payments [get]
func paymentsHandler(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		workerID := strings.TrimSpace(r.URL.Query().Get("worker_id"))
		events, err := store.ListPaymentEvents(r.Context(), workerID)
		if err != nil {
			http.Error(w, "failed to load payments", http.StatusInternalServerError)
			return
		}
		writeJSON(w, events, http.StatusOK)
	}
}

// processPaymentsHandler godoc
// @Summary      Process pending payments
// @Tags         payments
// @Produce      json
// @Success      200  {object}  ProcessPaymentsResponse
// @Failure      405  {string}  string
// @Router       /api/payments/process [post]
func processPaymentsHandler(engine *scheduler.Engine, store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		processedCount, err := runPaymentSweep(r.Context(), engine, store)
		if err != nil {
			log.Printf("persist payments failed: err=%v", err)
		}
		writeJSON(w, ProcessPaymentsResponse{
			ProcessedCount: processedCount,
		}, http.StatusOK)
	}
}

func triggerPaymentProcessingAsync(engine *scheduler.Engine, store *postgres.Store) {
	go func() {
		totalProcessed := 0
		for {
			processedCount, err := runPaymentSweep(context.Background(), engine, store)
			if err != nil {
				log.Printf("async payment sweep failed: %v", err)
				return
			}
			totalProcessed += processedCount

			if pendingPaymentCount(engine.PaymentQueueSnapshot()) == 0 {
				if totalProcessed > 0 {
					log.Printf("async payment sweep processed=%d", totalProcessed)
				}
				return
			}

			time.Sleep(2 * time.Second)
		}
	}()
}

func startPaymentsProcessor(engine *scheduler.Engine, store *postgres.Store, interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				processedCount, err := runPaymentSweep(ctx, engine, store)
				if err != nil {
					log.Printf("background payment sweep failed: %v", err)
					continue
				}
				if processedCount > 0 {
					log.Printf("background payment sweep processed=%d", processedCount)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return cancel
}

func startAssignmentJanitor(engine *scheduler.Engine, interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				engine.CleanupExpiredAssignments()
			case <-ctx.Done():
				return
			}
		}
	}()

	return cancel
}

func runPaymentSweep(ctx context.Context, engine *scheduler.Engine, store *postgres.Store) (int, error) {
	processedCount := engine.ProcessPayments()
	if err := store.UpsertPaymentEvents(ctx, engine.PaymentQueueSnapshot()); err != nil {
		return processedCount, err
	}
	return processedCount, nil
}

func pendingPaymentCount(events []scheduler.PaymentEvent) int {
	count := 0
	for _, event := range events {
		if event.Status == "pending_x402_transfer" || event.Status == "retry" {
			count++
		}
	}
	return count
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

func bootstrapWorkflow(
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) error {
	bootPath := strings.TrimSpace(os.Getenv("WORKFLOW_BOOT_FILE"))
	if bootPath == "" {
		bootPath = filepath.Join("workflows", "prime-example", "prime-example.json")
	}
	topologyMode, err := store.GetTopologyMode(context.Background())
	if err != nil {
		log.Printf("failed to read topology mode from db: %v", err)
		topologyMode = ""
	}
	workflowManager.SetTopologyMode(scheduler.NormalizeTopologyMode(topologyMode))
	activeID, err := store.GetActiveWorkflowID(context.Background())
	if err != nil {
		log.Printf("failed to read active workflow id from db: %v", err)
		activeID = ""
	}
	if activeID != "" {
		activePath, err := resolveWorkflowSpecPathByID(activeID)
		if err == nil {
			bootPath = activePath
		} else {
			log.Printf("active workflow file missing, fallback to default: workflow_id=%s", activeID)
		}
	}

	spec, result, recovered, err := loadWorkflowFromPath(context.Background(), bootPath, engine, workflowManager, store)
	if err != nil {
		return fmt.Errorf("workflow bootstrap load failed (%s): %w", bootPath, err)
	}
	if err := store.SetActiveWorkflowID(context.Background(), spec.ID); err != nil {
		log.Printf("failed to persist active workflow id during bootstrap: %v", err)
	}
	if err := store.SetTopologyMode(context.Background(), string(workflowManager.TopologyMode())); err != nil {
		log.Printf("failed to persist topology mode during bootstrap: %v", err)
	}
	log.Printf(
		"workflow bootstrapped: file=%s workflow_id=%s topology_mode=%s topo_nodes=%d recovered_completed=%d initial_jobs=%d",
		bootPath,
		spec.ID,
		workflowManager.TopologyMode(),
		len(result.TopologicalOrder),
		recovered,
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
