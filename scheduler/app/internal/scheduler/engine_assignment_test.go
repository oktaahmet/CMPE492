package scheduler

import (
	"testing"
	"time"
)

func TestEngineAssignNextAllowsReplicationFanout(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		ReplicationFactor: 3,
		AssignmentTTL:     time.Minute,
	})
	job := Job{
		ID:         "job-fanout",
		WorkflowID: "wf",
		NodeID:     "n1",
		WasmURL:    "/job.wasm",
		RewardUSDC: "0.1",
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
		ReplicationFactor: 1,
		AssignmentTTL:     50 * time.Millisecond,
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

func TestEngineCleanupExpiredAssignmentsPrunesOfflineWorkers(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		ReplicationFactor: 1,
		AssignmentTTL:     50 * time.Millisecond,
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
