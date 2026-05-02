package scheduler

import (
	"testing"
	"time"
)

func TestEngineAssignNextAllowsReplicationFanout(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: time.Minute,
	})
	job := Job{
		ID:                "job-fanout",
		WorkflowID:        "wf",
		NodeID:            "n1",
		WasmURL:           "/job.wasm",
		RewardUSDC:        "0.1",
		ReplicationFactor: 3,
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	for _, workerID := range []string{"w1", "w2", "w3"} {
		assignment, ok := engine.AssignNext(workerID)
		if !ok {
			t.Fatalf("expected assignment for %s", workerID)
		}
		if assignment.JobID != job.ID {
			t.Fatalf("unexpected job for %s: %#v", workerID, assignment)
		}
	}

	if assignment, ok := engine.AssignNext("w4"); ok {
		t.Fatalf("expected replication limit to block fourth worker, got %#v", assignment)
	}
}

func TestEngineCleanupExpiredAssignmentsReleasesStaleAssignments(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: 50 * time.Millisecond,
	})
	job := Job{
		ID:         "job-expire",
		WorkflowID: "wf",
		NodeID:     "n1",
		WasmURL:    "/job.wasm",
		RewardUSDC: "0.1",
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if _, ok := engine.AssignNext("w1"); !ok {
		t.Fatalf("expected initial assignment")
	}

	engine.mu.Lock()
	state := engine.jobs[job.ID]
	if state == nil {
		engine.mu.Unlock()
		t.Fatalf("job state missing")
	}
	state.assignments["w1"] = time.Now().Add(-time.Second)
	engine.mu.Unlock()

	engine.CleanupExpiredAssignments()

	if assignment, ok := engine.AssignNext("w2"); !ok {
		t.Fatalf("expected reassignment after cleanup")
	} else if assignment.JobID != job.ID {
		t.Fatalf("unexpected reassigned job: %#v", assignment)
	}
}

func TestEngineRejectsLateSubmitAfterAbandonedAssignmentIsReassigned(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: 50 * time.Millisecond,
	})
	job := Job{
		ID:         "job-abandoned",
		WorkflowID: "wf",
		NodeID:     "n1",
		WasmURL:    "/job.wasm",
		RewardUSDC: "0.1",
		ResultSchema: map[string]PayloadFieldRule{
			"output": {Type: "string", Required: true},
		},
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if _, ok := engine.AssignNext("browser-that-stopped"); !ok {
		t.Fatalf("expected first browser assignment")
	}
	if assignment, ok := engine.AssignNext("replacement-before-ttl"); ok {
		t.Fatalf("expected assignment to stay reserved until TTL, got %#v", assignment)
	}

	engine.mu.Lock()
	state := engine.jobs[job.ID]
	if state == nil {
		engine.mu.Unlock()
		t.Fatalf("job state missing")
	}
	state.assignments["browser-that-stopped"] = time.Now().Add(-time.Second)
	engine.mu.Unlock()

	if assignment, ok := engine.AssignNext("replacement-after-ttl"); !ok {
		t.Fatalf("expected replacement assignment after stale browser TTL")
	} else if assignment.JobID != job.ID {
		t.Fatalf("unexpected replacement assignment: %#v", assignment)
	}

	if _, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "browser-that-stopped",
		ResultSig:     "late-result",
		ResultPayload: map[string]any{"output": "late"},
	}); err == nil {
		t.Fatalf("expected late submit from expired assignment to be rejected")
	}

	decision, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "replacement-after-ttl",
		ResultSig:     "replacement-result",
		ResultPayload: map[string]any{"output": "ok"},
	})
	if err != nil {
		t.Fatalf("SubmitResult(replacement) error = %v", err)
	}
	if !decision.Finalized {
		t.Fatalf("expected replacement result to finalize job: %#v", decision)
	}
	events := engine.PaymentQueueSnapshot()
	if len(events) != 1 || events[0].WorkerID != "replacement-after-ttl" {
		t.Fatalf("expected only replacement worker to be paid, got %#v", events)
	}
}

func TestEngineSubmittedResultSurvivesWorkerExpiryUntilQuorum(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: 50 * time.Millisecond,
	})
	job := Job{
		ID:                "job-submitted-survives",
		WorkflowID:        "wf",
		NodeID:            "n1",
		WasmURL:           "/job.wasm",
		RewardUSDC:        "0.1",
		ReplicationFactor: 3,
		ResultSchema: map[string]PayloadFieldRule{
			"output": {Type: "number", Required: true},
		},
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	for _, workerID := range []string{"w1", "w2"} {
		if _, ok := engine.AssignNext(workerID); !ok {
			t.Fatalf("expected assignment for %s", workerID)
		}
	}

	first, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w1",
		ResultSig:     "sig",
		ResultPayload: map[string]any{"output": float64(7)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w1) error = %v", err)
	}
	if first.Finalized {
		t.Fatalf("first result should wait for quorum: %#v", first)
	}

	engine.mu.Lock()
	if worker, ok := engine.workers["w1"]; ok {
		worker.LastHeartbeat = time.Now().Add(-time.Second)
		engine.workers["w1"] = worker
	}
	if state := engine.jobs[job.ID]; state != nil {
		state.assignments["w2"] = time.Now().Add(-time.Second)
	}
	engine.mu.Unlock()

	engine.CleanupExpiredAssignments()

	if _, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w2",
		ResultSig:     "sig",
		ResultPayload: map[string]any{"output": float64(7)},
	}); err == nil {
		t.Fatalf("expected expired unsubmitted assignment to be rejected")
	}

	if _, ok := engine.AssignNext("w3"); !ok {
		t.Fatalf("expected stale unsubmitted replica slot to be reassigned")
	}
	second, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w3",
		ResultSig:     "sig",
		ResultPayload: map[string]any{"output": float64(7)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w3) error = %v", err)
	}
	if !second.Finalized {
		t.Fatalf("submitted w1 result should still count toward quorum: %#v", second)
	}
}

func TestEngineCleanupExpiredAssignmentsPrunesOfflineWorkers(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: 50 * time.Millisecond,
	})

	engine.RegisterOrHeartbeat("w1")
	engine.RegisterOrHeartbeat("w2")

	engine.mu.Lock()
	worker := engine.workers["w1"]
	worker.LastHeartbeat = time.Now().Add(-time.Second)
	engine.workers["w1"] = worker
	engine.mu.Unlock()

	engine.CleanupExpiredAssignments()

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if _, exists := engine.workers["w1"]; exists {
		t.Fatalf("expected stale worker w1 to be pruned")
	}
	if _, exists := engine.workers["w2"]; !exists {
		t.Fatalf("expected active worker w2 to remain registered")
	}
}
