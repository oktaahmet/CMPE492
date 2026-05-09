package scheduler

import (
	"reflect"
	"testing"
	"time"
)

func TestWorkflowManagerAndEngineProgressBrowserWorkflow(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: time.Minute,
	})
	manager := NewWorkflowManager()

	result, jobs, err := manager.LoadWorkflow(WorkflowSpec{
		ID: "wf-engine-integration",
		Nodes: []WorkflowNode{
			{
				ID:                "produce",
				WasmURL:           "/produce.wasm",
				RewardUSDC:        "0.01",
				ReplicationFactor: 2,
				ResultSchema: map[string]PayloadFieldRule{
					"output": {Type: "object", Required: true},
				},
			},
			{
				ID:                "sample",
				DependsOn:         []string{"produce"},
				WasmURL:           "/sample.wasm",
				RewardUSDC:        "0.02",
				ReplicationFactor: 3,
				AcceptancePolicy:  AcceptancePolicyCollectAll,
				ResultSchema: map[string]PayloadFieldRule{
					"output": {Type: "object", Required: true},
				},
			},
			{
				ID:                "reduce",
				DependsOn:         []string{"sample"},
				WasmURL:           "/reduce.wasm",
				RewardUSDC:        "0.03",
				ReplicationFactor: 2,
				ResultSchema: map[string]PayloadFieldRule{
					"output": {Type: "string", Required: true},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("LoadWorkflow() error = %v", err)
	}
	if !reflect.DeepEqual(result.EnqueuedNodes, []string{"produce"}) {
		t.Fatalf("expected only root node to enqueue first, got %#v", result.EnqueuedNodes)
	}
	mustEnqueueAll(t, engine, jobs)

	produceDecision := submitMatchingResults(t, engine, "wf-engine-integration:produce", []string{"w1", "w2"}, map[string]any{
		"output": map[string]any{"value": float64(7)},
	})
	if !produceDecision.Finalized {
		t.Fatalf("produce should finalize after quorum: %#v", produceDecision)
	}
	produceOutput, ok := engine.FinalizedOutput("wf-engine-integration:produce")
	if !ok {
		t.Fatalf("expected produce finalized output")
	}

	nextJobs, err := manager.OnJobFinalized("wf-engine-integration:produce", produceOutput)
	if err != nil {
		t.Fatalf("OnJobFinalized(produce) error = %v", err)
	}
	if len(nextJobs) != 1 || nextJobs[0].NodeID != "sample" {
		t.Fatalf("expected sample to unlock, got %#v", nextJobs)
	}
	if len(nextJobs[0].Dependencies) != 1 || nextJobs[0].Dependencies[0].NodeID != "produce" {
		t.Fatalf("expected sample dependency on produce, got %#v", nextJobs[0].Dependencies)
	}
	mustEnqueueAll(t, engine, nextJobs)

	for i, workerID := range []string{"w3", "w4", "w5"} {
		if _, ok := engine.AssignNext(workerID); !ok {
			t.Fatalf("expected sample assignment for %s", workerID)
		}
		decision, err := engine.SubmitResult(ResultSubmission{
			JobID:     "wf-engine-integration:sample",
			WorkerID:  workerID,
			ResultSig: "ignored-by-canonical-payload",
			ResultPayload: map[string]any{
				"output": map[string]any{"sample": float64(i + 1)},
			},
		})
		if err != nil {
			t.Fatalf("SubmitResult(sample/%s) error = %v", workerID, err)
		}
		if i < 2 && decision.Finalized {
			t.Fatalf("collect_all sample finalized too early: %#v", decision)
		}
		if i == 2 && !decision.Finalized {
			t.Fatalf("collect_all sample should finalize after all replicas")
		}
	}

	sampleOutput, ok := engine.FinalizedOutput("wf-engine-integration:sample")
	if !ok {
		t.Fatalf("expected sample finalized output")
	}
	wrapped, ok := sampleOutput["output"].(map[string]any)
	if !ok || wrapped["sample_count"] != 3 {
		t.Fatalf("expected collect_all wrapper with 3 samples, got %#v", sampleOutput)
	}

	nextJobs, err = manager.OnJobFinalized("wf-engine-integration:sample", sampleOutput)
	if err != nil {
		t.Fatalf("OnJobFinalized(sample) error = %v", err)
	}
	if len(nextJobs) != 1 || nextJobs[0].NodeID != "reduce" {
		t.Fatalf("expected reduce to unlock, got %#v", nextJobs)
	}
	mustEnqueueAll(t, engine, nextJobs)

	reduceDecision := submitMatchingResults(t, engine, "wf-engine-integration:reduce", []string{"w6", "w7"}, map[string]any{
		"output": "done",
	})
	if !reduceDecision.Finalized {
		t.Fatalf("reduce should finalize after quorum: %#v", reduceDecision)
	}

	if got := len(engine.PaymentQueueSnapshot()); got != 7 {
		t.Fatalf("expected payment event per accepted worker, got %d", got)
	}
}

func mustEnqueueAll(t *testing.T, engine *Engine, jobs []Job) {
	t.Helper()
	for _, job := range jobs {
		if err := engine.Enqueue(job); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", job.ID, err)
		}
	}
}

func submitMatchingResults(t *testing.T, engine *Engine, jobID string, workerIDs []string, payload map[string]any) Decision {
	t.Helper()

	var decision Decision
	for _, workerID := range workerIDs {
		if _, ok := engine.AssignNext(workerID); !ok {
			t.Fatalf("expected assignment for %s on %s", workerID, jobID)
		}
		var err error
		decision, err = engine.SubmitResult(ResultSubmission{
			JobID:         jobID,
			WorkerID:      workerID,
			ResultSig:     "ignored-by-canonical-payload",
			ResultPayload: payload,
		})
		if err != nil {
			t.Fatalf("SubmitResult(%s/%s) error = %v", jobID, workerID, err)
		}
	}
	return decision
}
