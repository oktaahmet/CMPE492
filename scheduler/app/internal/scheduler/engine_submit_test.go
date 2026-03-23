package scheduler

import (
	"reflect"
	"testing"
	"time"
)

func TestValidateResultPayloadEnforcesSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]PayloadFieldRule{
		"output":  {Type: "number", Required: true},
		"summary": {Type: "object"},
		"tags":    {Type: "array"},
	}

	if err := validateResultPayload(map[string]any{
		"output":  float64(12),
		"summary": map[string]any{"ok": true},
		"tags":    []any{"a", "b"},
	}, schema); err != nil {
		t.Fatalf("expected valid payload, got %v", err)
	}

	if err := validateResultPayload(map[string]any{
		"summary": map[string]any{},
	}, schema); err == nil {
		t.Fatalf("expected missing required output to fail")
	}

	if err := validateResultPayload(map[string]any{
		"output": "12",
	}, schema); err == nil {
		t.Fatalf("expected wrong output type to fail")
	}
}

func TestEngineSubmitResultFinalizesOnQuorumAndCreatesPaymentEvents(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		ReplicationFactor: 3,
		AssignmentTTL:     time.Minute,
	})
	job := Job{
		ID:         "job-finalize",
		WorkflowID: "wf",
		NodeID:     "n1",
		WasmURL:    "/job.wasm",
		RewardUSDC: "0.25",
		ResultSchema: map[string]PayloadFieldRule{
			"output": {Type: "number", Required: true},
		},
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	for _, workerID := range []string{"w1", "w2", "w3"} {
		if _, ok := engine.AssignNext(workerID); !ok {
			t.Fatalf("expected assignment for %s", workerID)
		}
	}

	first, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w1",
		ResultSig:     "sig-a",
		ResultPayload: map[string]any{"output": float64(99)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w1) error = %v", err)
	}
	if first.Finalized {
		t.Fatalf("job should not finalize after first matching result")
	}

	second, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w2",
		ResultSig:     "sig-a",
		ResultPayload: map[string]any{"output": float64(99)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w2) error = %v", err)
	}
	if !second.Finalized {
		t.Fatalf("job should finalize on quorum")
	}
	expectedDigest, err := canonicalSubmissionDigest(map[string]any{"output": float64(99)})
	if err != nil {
		t.Fatalf("canonicalSubmissionDigest() error = %v", err)
	}
	if second.AcceptedResult != expectedDigest || second.AcceptedWorkers != 2 {
		t.Fatalf("unexpected decision: %#v", second)
	}
	if !reflect.DeepEqual(second.AcceptedPayload, map[string]any{"output": float64(99)}) {
		t.Fatalf("unexpected accepted payload: %#v", second.AcceptedPayload)
	}

	output, ok := engine.FinalizedOutput(job.ID)
	if !ok {
		t.Fatalf("FinalizedOutput() expected finalized output")
	}
	if !reflect.DeepEqual(output, map[string]any{"output": float64(99)}) {
		t.Fatalf("unexpected finalized output: %#v", output)
	}

	events := engine.PaymentQueueSnapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 payment events, got %d (%#v)", len(events), events)
	}
	if events[0].WorkerID != "w1" || events[1].WorkerID != "w2" {
		t.Fatalf("unexpected payment recipients: %#v", events)
	}
	if events[0].AcceptedHash != expectedDigest || events[1].AcceptedHash != expectedDigest {
		t.Fatalf("unexpected payment accepted hashes: %#v", events)
	}
	if events[0].Status != "pending_x402_transfer" || events[1].Status != "pending_x402_transfer" {
		t.Fatalf("unexpected payment statuses: %#v", events)
	}
}

func TestEngineSubmitResultUsesCanonicalPayloadForConsensus(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		ReplicationFactor: 3,
		AssignmentTTL:     time.Minute,
	})
	job := Job{
		ID:         "job-consensus",
		WorkflowID: "wf",
		NodeID:     "n1",
		WasmURL:    "/job.wasm",
		RewardUSDC: "0.25",
		ResultSchema: map[string]PayloadFieldRule{
			"output": {Type: "number", Required: true},
		},
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	for _, workerID := range []string{"w1", "w2", "w3"} {
		if _, ok := engine.AssignNext(workerID); !ok {
			t.Fatalf("expected assignment for %s", workerID)
		}
	}

	first, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w1",
		ResultSig:     "shared-sig",
		ResultPayload: map[string]any{"output": float64(11)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w1) error = %v", err)
	}
	if first.Finalized {
		t.Fatalf("job should not finalize after one payload")
	}

	second, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w2",
		ResultSig:     "shared-sig",
		ResultPayload: map[string]any{"output": float64(22)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w2) error = %v", err)
	}
	if second.Finalized {
		t.Fatalf("job should not finalize when only claimed sig matches")
	}

	third, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w3",
		ResultSig:     "different-claim",
		ResultPayload: map[string]any{"output": float64(22)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w3) error = %v", err)
	}
	if !third.Finalized {
		t.Fatalf("job should finalize when canonical payload reaches quorum")
	}
	if !reflect.DeepEqual(third.AcceptedPayload, map[string]any{"output": float64(22)}) {
		t.Fatalf("unexpected accepted payload: %#v", third.AcceptedPayload)
	}
	if third.AcceptedResult == "shared-sig" || third.AcceptedResult == "different-claim" {
		t.Fatalf("accepted result should be server-derived payload digest, got %q", third.AcceptedResult)
	}
}

func TestEngineSubmitResultResetsRoundAfterSplitDecision(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		ReplicationFactor: 3,
		AssignmentTTL:     time.Minute,
	})
	job := Job{
		ID:         "job-split",
		WorkflowID: "wf",
		NodeID:     "n1",
		WasmURL:    "/job.wasm",
		RewardUSDC: "0.1",
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	for _, workerID := range []string{"w1", "w2", "w3"} {
		if _, ok := engine.AssignNext(workerID); !ok {
			t.Fatalf("expected assignment for %s", workerID)
		}
	}

	for i, workerID := range []string{"w1", "w2", "w3"} {
		decision, err := engine.SubmitResult(ResultSubmission{
			JobID:         job.ID,
			WorkerID:      workerID,
			ResultSig:     string(rune('a' + i)),
			ResultPayload: map[string]any{"output": float64(i)},
		})
		if err != nil {
			t.Fatalf("SubmitResult(%s) error = %v", workerID, err)
		}
		if decision.Finalized {
			t.Fatalf("job should not finalize during split round: %#v", decision)
		}
	}

	engine.mu.Lock()
	state := engine.jobs[job.ID]
	if state == nil {
		engine.mu.Unlock()
		t.Fatalf("job state missing after split round")
	}
	if len(state.assignments) != 0 || len(state.submitted) != 0 || len(state.submittedPayload) != 0 {
		engine.mu.Unlock()
		t.Fatalf("round state should be reset after split decision: assignments=%d submitted=%d payloads=%d", len(state.assignments), len(state.submitted), len(state.submittedPayload))
	}
	engine.mu.Unlock()

	for _, workerID := range []string{"w1", "w2", "w3"} {
		if assignment, ok := engine.AssignNext(workerID); !ok {
			t.Fatalf("expected split-round worker %s to be eligible again", workerID)
		} else if assignment.JobID != job.ID {
			t.Fatalf("unexpected reassigned job for %s: %#v", workerID, assignment)
		}
	}
}

func TestEngineSubmitResultRejectsUnassignedAndIgnoresDuplicateOrLateSubmit(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		ReplicationFactor: 2,
		AssignmentTTL:     time.Minute,
	})
	job := Job{
		ID:         "job-negative-submit",
		WorkflowID: "wf",
		NodeID:     "n1",
		WasmURL:    "/job.wasm",
		RewardUSDC: "0.1",
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if _, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "intruder",
		ResultSig:     "sig-x",
		ResultPayload: map[string]any{"output": float64(1)},
	}); err == nil {
		t.Fatal("expected unassigned worker to be rejected")
	}

	for _, workerID := range []string{"w1", "w2"} {
		if _, ok := engine.AssignNext(workerID); !ok {
			t.Fatalf("expected assignment for %s", workerID)
		}
	}

	first, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w1",
		ResultSig:     "sig-a",
		ResultPayload: map[string]any{"output": float64(7)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w1) error = %v", err)
	}
	if first.Finalized {
		t.Fatalf("job should not finalize after first submit")
	}

	duplicate, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w1",
		ResultSig:     "sig-b",
		ResultPayload: map[string]any{"output": float64(999)},
	})
	if err != nil {
		t.Fatalf("duplicate SubmitResult(w1) error = %v", err)
	}
	if !reflect.DeepEqual(duplicate, first) {
		t.Fatalf("duplicate submit should not mutate decision: got %#v want %#v", duplicate, first)
	}

	final, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w2",
		ResultSig:     "sig-a",
		ResultPayload: map[string]any{"output": float64(7)},
	})
	if err != nil {
		t.Fatalf("SubmitResult(w2) error = %v", err)
	}
	if !final.Finalized {
		t.Fatalf("expected quorum to finalize job")
	}

	late, err := engine.SubmitResult(ResultSubmission{
		JobID:         job.ID,
		WorkerID:      "w3",
		ResultSig:     "sig-a",
		ResultPayload: map[string]any{"output": float64(7)},
	})
	if err != nil {
		t.Fatalf("late SubmitResult(w3) error = %v", err)
	}
	if !reflect.DeepEqual(late, final) {
		t.Fatalf("late submit should return finalized decision unchanged: got %#v want %#v", late, final)
	}
	if events := engine.PaymentQueueSnapshot(); len(events) != 2 {
		t.Fatalf("expected exactly 2 payment events after duplicate/late submit checks, got %#v", events)
	}
}
