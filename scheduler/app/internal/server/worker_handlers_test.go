package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"x402-scheduler/internal/scheduler"
)

func TestWorkerHTTPHandlersRegisterPullAndSubmitNonFinalizedResult(t *testing.T) {
	t.Parallel()

	engine := scheduler.NewEngine(scheduler.Config{
		AssignmentTTL: time.Minute,
	})
	workflowManager := scheduler.NewWorkflowManager()
	job := scheduler.Job{
		ID:                "job-http-flow",
		WorkflowID:        "wf-http",
		NodeID:            "node-a",
		WasmURL:           "/node-a.wasm",
		RewardUSDC:        "0.01",
		ReplicationFactor: 2,
		ResultSchema: map[string]scheduler.PayloadFieldRule{
			"output": {Type: "number", Required: true},
		},
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	workerID := "0x0000000000000000000000000000000000000001"
	registerBody := mustJSONBody(t, RegisterWorkerRequest{WorkerID: workerID})
	registerReq := httptest.NewRequest(http.MethodPost, "/api/workers/register", registerBody)
	registerRec := httptest.NewRecorder()
	registerWorkerHandler(engine, nil).ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", registerRec.Code, registerRec.Body.String())
	}

	pullReq := httptest.NewRequest(http.MethodGet, "/api/pull?worker_id="+workerID, nil)
	pullRec := httptest.NewRecorder()
	pullHandler(engine, nil).ServeHTTP(pullRec, pullReq)
	if pullRec.Code != http.StatusOK {
		t.Fatalf("pull status = %d body=%s", pullRec.Code, pullRec.Body.String())
	}
	var assignment scheduler.Assignment
	if err := json.NewDecoder(pullRec.Body).Decode(&assignment); err != nil {
		t.Fatalf("decode assignment: %v", err)
	}
	if assignment.JobID != job.ID || assignment.RequiredRep != 2 {
		t.Fatalf("unexpected assignment: %#v", assignment)
	}

	resultBody := mustJSONBody(t, scheduler.ResultSubmission{
		JobID:         job.ID,
		WorkerID:      workerID,
		ResultSig:     "worker-claim",
		ResultPayload: map[string]any{"output": float64(12)},
	})
	resultReq := httptest.NewRequest(http.MethodPost, "/api/result", resultBody)
	resultRec := httptest.NewRecorder()
	resultHandler(engine, workflowManager, nil, 1<<20, nil).ServeHTTP(resultRec, resultReq)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("result status = %d body=%s", resultRec.Code, resultRec.Body.String())
	}
	var decision scheduler.Decision
	if err := json.NewDecoder(resultRec.Body).Decode(&decision); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if decision.Finalized {
		t.Fatalf("first of two replicas should not finalize: %#v", decision)
	}

	statsRec := httptest.NewRecorder()
	statsHandler(engine).ServeHTTP(statsRec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	if statsRec.Code != http.StatusOK {
		t.Fatalf("stats status = %d body=%s", statsRec.Code, statsRec.Body.String())
	}
	var stats scheduler.Stats
	if err := json.NewDecoder(statsRec.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.TotalJobs != 1 || stats.FinalizedJobs != 0 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestWorkerHTTPHandlersRejectBadMethodsAndOversizedResults(t *testing.T) {
	t.Parallel()

	engine := scheduler.NewEngine(scheduler.Config{
		AssignmentTTL: time.Minute,
	})
	workflowManager := scheduler.NewWorkflowManager()

	registerRec := httptest.NewRecorder()
	registerWorkerHandler(engine, nil).ServeHTTP(registerRec, httptest.NewRequest(http.MethodGet, "/api/workers/register", nil))
	if registerRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected register method rejection, got %d", registerRec.Code)
	}

	oversizedBody := bytes.NewBufferString(`{"job_id":"job","worker_id":"0x0000000000000000000000000000000000000001","result_sig":"sig","result_payload":{"output":"too large"}}`)
	resultReq := httptest.NewRequest(http.MethodPost, "/api/result", oversizedBody)
	resultRec := httptest.NewRecorder()
	resultHandler(engine, workflowManager, nil, 32, nil).ServeHTTP(resultRec, resultReq)
	if resultRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected oversized result rejection, got %d body=%s", resultRec.Code, resultRec.Body.String())
	}
}

func TestWorkerHTTPHandlersReassignAbandonedPullAfterTTL(t *testing.T) {
	t.Parallel()

	engine := scheduler.NewEngine(scheduler.Config{
		AssignmentTTL: time.Millisecond,
	})
	workflowManager := scheduler.NewWorkflowManager()
	job := scheduler.Job{
		ID:         "job-http-abandoned",
		WorkflowID: "wf-http",
		NodeID:     "node-a",
		WasmURL:    "/node-a.wasm",
		RewardUSDC: "0.01",
		ResultSchema: map[string]scheduler.PayloadFieldRule{
			"output": {Type: "string", Required: true},
		},
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	firstWorker := "0x0000000000000000000000000000000000000001"
	secondWorker := "0x0000000000000000000000000000000000000002"

	firstPull := httptest.NewRecorder()
	pullHandler(engine, nil).ServeHTTP(firstPull, httptest.NewRequest(http.MethodGet, "/api/pull?worker_id="+firstWorker, nil))
	if firstPull.Code != http.StatusOK {
		t.Fatalf("first pull status = %d body=%s", firstPull.Code, firstPull.Body.String())
	}

	blockedPull := httptest.NewRecorder()
	pullHandler(engine, nil).ServeHTTP(blockedPull, httptest.NewRequest(http.MethodGet, "/api/pull?worker_id="+secondWorker, nil))
	if blockedPull.Code != http.StatusNoContent {
		t.Fatalf("expected second worker to wait before TTL, got %d body=%s", blockedPull.Code, blockedPull.Body.String())
	}

	time.Sleep(5 * time.Millisecond)

	reassignedPull := httptest.NewRecorder()
	pullHandler(engine, nil).ServeHTTP(reassignedPull, httptest.NewRequest(http.MethodGet, "/api/pull?worker_id="+secondWorker, nil))
	if reassignedPull.Code != http.StatusOK {
		t.Fatalf("expected reassignment after TTL, got %d body=%s", reassignedPull.Code, reassignedPull.Body.String())
	}
	var assignment scheduler.Assignment
	if err := json.NewDecoder(reassignedPull.Body).Decode(&assignment); err != nil {
		t.Fatalf("decode reassigned assignment: %v", err)
	}
	if assignment.JobID != job.ID {
		t.Fatalf("unexpected reassigned assignment: %#v", assignment)
	}

	lateResultBody := mustJSONBody(t, scheduler.ResultSubmission{
		JobID:         job.ID,
		WorkerID:      firstWorker,
		ResultSig:     "late",
		ResultPayload: map[string]any{"output": "late"},
	})
	lateResultReq := httptest.NewRequest(http.MethodPost, "/api/result", lateResultBody)
	lateResultRec := httptest.NewRecorder()
	resultHandler(engine, workflowManager, nil, 1<<20, nil).ServeHTTP(lateResultRec, lateResultReq)
	if lateResultRec.Code != http.StatusBadRequest {
		t.Fatalf("expected stale worker submit rejection, got %d body=%s", lateResultRec.Code, lateResultRec.Body.String())
	}
}

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	healthHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if resp.Status != "ok" || resp.Checks["http"] != "ok" {
		t.Fatalf("unexpected health response: %#v", resp)
	}
}

func mustJSONBody(t *testing.T, value any) *bytes.Reader {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json body: %v", err)
	}
	return bytes.NewReader(raw)
}
