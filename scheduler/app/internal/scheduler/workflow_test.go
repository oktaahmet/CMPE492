package scheduler

import (
	"reflect"
	"testing"
)

func TestValidateWorkflowSpecNormalizesTraits(t *testing.T) {
	spec, err := ValidateWorkflowSpec(WorkflowSpec{
		ID: "wf-traits-test",
		Nodes: []WorkflowNode{
			{
				ID:               "node-a",
				WasmURL:          "/a.wasm",
				RewardUSDC:       "0.01",
				AcceptancePolicy: AcceptancePolicyConsensus,
				Traits:           []string{" stochastic ", "SIMULATION", "", "stochastic"},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateWorkflowSpec returned error: %v", err)
	}

	if len(spec.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(spec.Nodes))
	}

	got := spec.Nodes[0].Traits
	want := []string{"simulation", "stochastic"}
	if len(got) != len(want) {
		t.Fatalf("expected %d traits, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected trait %q at index %d, got %q", want[i], i, got[i])
		}
	}
}

func TestValidateWorkflowSpecDefaultsExecutionTargetToBrowserWorker(t *testing.T) {
	spec, err := ValidateWorkflowSpec(WorkflowSpec{
		ID: "wf-target-default-test",
		Nodes: []WorkflowNode{
			{
				ID:         "node-a",
				WasmURL:    "/a.wasm",
				RewardUSDC: "0.01",
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateWorkflowSpec returned error: %v", err)
	}

	if got := spec.Nodes[0].ExecutionTarget; got != ExecutionTargetBrowserWorker {
		t.Fatalf("expected default execution target %q, got %q", ExecutionTargetBrowserWorker, got)
	}
}

func TestValidateWorkflowSpecExpandsShardedNodesAndDependencies(t *testing.T) {
	spec, err := ValidateWorkflowSpec(WorkflowSpec{
		ID: "wf-sharded-validation-test",
		Nodes: []WorkflowNode{
			{ID: "prepare", WasmURL: "/prepare.wasm", RewardUSDC: "0.01"},
			{
				ID:                "prime-shard",
				DependsOn:         []string{"prepare"},
				WasmURL:           "/prime.wasm",
				Args:              []any{map[string]any{"range_size": 1000}},
				ShardCount:        3,
				ReplicationFactor: 3,
				AcceptancePolicy:  AcceptancePolicyConsensus,
				RewardUSDC:        "0.02",
			},
			{ID: "reduce", DependsOn: []string{"prime-shard"}, WasmURL: "/reduce.wasm", RewardUSDC: "0.01"},
		},
	})
	if err != nil {
		t.Fatalf("ValidateWorkflowSpec returned error: %v", err)
	}

	gotIDs := make([]string, 0, len(spec.Nodes))
	byID := map[string]WorkflowNode{}
	for _, node := range spec.Nodes {
		gotIDs = append(gotIDs, node.ID)
		byID[node.ID] = node
	}
	wantIDs := []string{"prepare", "prime-shard-0", "prime-shard-1", "prime-shard-2", "reduce"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("expected expanded node ids %#v, got %#v", wantIDs, gotIDs)
	}

	for i, nodeID := range []string{"prime-shard-0", "prime-shard-1", "prime-shard-2"} {
		node := byID[nodeID]
		if node.ShardCount != 0 {
			t.Fatalf("expected expanded node %s to clear shard_count, got %d", nodeID, node.ShardCount)
		}
		if !reflect.DeepEqual(node.DependsOn, []string{"prepare"}) {
			t.Fatalf("expected %s to depend on prepare, got %#v", nodeID, node.DependsOn)
		}
		args, ok := node.Args[0].(map[string]any)
		if !ok {
			t.Fatalf("expected %s first arg to be an object, got %#v", nodeID, node.Args)
		}
		if args["range_size"] != 1000 || args["shard_index"] != i {
			t.Fatalf("unexpected args for %s: %#v", nodeID, args)
		}
	}

	wantReduceDeps := []string{"prime-shard-0", "prime-shard-1", "prime-shard-2"}
	if !reflect.DeepEqual(byID["reduce"].DependsOn, wantReduceDeps) {
		t.Fatalf("expected reducer deps %#v, got %#v", wantReduceDeps, byID["reduce"].DependsOn)
	}
}

func TestValidateWorkflowSpecRejectsInvalidNodeModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node WorkflowNode
	}{
		{
			name: "execution_target",
			node: WorkflowNode{
				ID:              "node-a",
				WasmURL:         "/a.wasm",
				ExecutionTarget: ExecutionTarget("elsewhere"),
				RewardUSDC:      "0.01",
			},
		},
		{
			name: "acceptance_policy",
			node: WorkflowNode{
				ID:               "node-a",
				WasmURL:          "/a.wasm",
				AcceptancePolicy: AcceptancePolicy("best_effort"),
				RewardUSDC:       "0.01",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateWorkflowSpec(WorkflowSpec{
				ID:    "wf-invalid-" + tc.name,
				Nodes: []WorkflowNode{tc.node},
			})
			if err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestValidateWorkflowSpecRejectsServerReplicationAboveOne(t *testing.T) {
	_, err := ValidateWorkflowSpec(WorkflowSpec{
		ID: "wf-server-replication-test",
		Nodes: []WorkflowNode{
			{
				ID:                "node-a",
				WasmURL:           "/a.wasm",
				ExecutionTarget:   ExecutionTargetServer,
				ReplicationFactor: 2,
				RewardUSDC:        "0.01",
			},
		},
	})
	if err == nil {
		t.Fatalf("expected server node validation error")
	}
}

func TestValidateWorkflowSpecNormalizesArtifacts(t *testing.T) {
	spec, err := ValidateWorkflowSpec(WorkflowSpec{
		ID: "wf-artifact-test",
		Artifacts: []WorkflowArtifact{
			{ID: "numbers", File: "numbers.txt"},
		},
		Nodes: []WorkflowNode{
			{
				ID:            "node-a",
				WasmURL:       "/a.wasm",
				UsesArtifacts: []string{"numbers", "numbers", ""},
				RewardUSDC:    "0.01",
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateWorkflowSpec returned error: %v", err)
	}
	if len(spec.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(spec.Artifacts))
	}
	if got := spec.Artifacts[0].Path; got != "data/numbers.txt" {
		t.Fatalf("expected normalized artifact path, got %q", got)
	}
	if got := spec.Nodes[0].UsesArtifacts; len(got) != 1 || got[0] != "numbers" {
		t.Fatalf("expected normalized uses_artifacts, got %#v", got)
	}
}

func TestValidateWorkflowSpecRejectsUnknownArtifactUse(t *testing.T) {
	_, err := ValidateWorkflowSpec(WorkflowSpec{
		ID: "wf-artifact-missing-test",
		Nodes: []WorkflowNode{
			{
				ID:            "node-a",
				WasmURL:       "/a.wasm",
				UsesArtifacts: []string{"numbers"},
				RewardUSDC:    "0.01",
			},
		},
	})
	if err == nil {
		t.Fatalf("expected unknown artifact validation error")
	}
}

func TestValidateWorkflowSpecAllowsServerOutputArtifacts(t *testing.T) {
	spec, err := ValidateWorkflowSpec(WorkflowSpec{
		ID: "wf-output-artifact-test",
		Nodes: []WorkflowNode{
			{
				ID:              "node-a",
				WasmURL:         "/a.wasm",
				ExecutionTarget: ExecutionTargetServer,
				OutputArtifacts: []WorkflowArtifact{
					{ID: "report", File: "report.txt"},
				},
				RewardUSDC: "0.00",
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateWorkflowSpec returned error: %v", err)
	}
	if got := spec.Nodes[0].OutputArtifacts[0].Path; got != "report.txt" {
		t.Fatalf("expected normalized output artifact path, got %q", got)
	}
}

func TestValidateWorkflowSpecRejectsBrowserOutputArtifacts(t *testing.T) {
	_, err := ValidateWorkflowSpec(WorkflowSpec{
		ID: "wf-browser-output-artifact-test",
		Nodes: []WorkflowNode{
			{
				ID:              "node-a",
				WasmURL:         "/a.wasm",
				OutputArtifacts: []WorkflowArtifact{{ID: "report", Path: "report.txt"}},
				RewardUSDC:      "0.01",
			},
		},
	})
	if err == nil {
		t.Fatalf("expected browser output artifact validation error")
	}
}

func TestWorkflowManagerLoadWithCompletedResumesUnlockedChildren(t *testing.T) {
	t.Parallel()

	manager := NewWorkflowManager()
	result, jobs, err := manager.LoadWorkflowWithCompleted(WorkflowSpec{
		ID: "wf-resume-test",
		Nodes: []WorkflowNode{
			{ID: "root", WasmURL: "/root.wasm", RewardUSDC: "0.01"},
			{ID: "left", DependsOn: []string{"root"}, WasmURL: "/left.wasm", RewardUSDC: "0.01"},
			{ID: "right", DependsOn: []string{"root"}, WasmURL: "/right.wasm", RewardUSDC: "0.01"},
		},
	}, map[string]map[string]any{
		"root": {"output": "already done"},
	})
	if err != nil {
		t.Fatalf("LoadWorkflowWithCompleted() error = %v", err)
	}

	if !reflect.DeepEqual(result.EnqueuedNodes, []string{"left", "right"}) {
		t.Fatalf("expected resumed workflow to enqueue unlocked children, got %#v", result.EnqueuedNodes)
	}
	if len(jobs) != 2 || jobs[0].NodeID != "left" || jobs[1].NodeID != "right" {
		t.Fatalf("unexpected resumed jobs: %#v", jobs)
	}

	snapshot, ok := manager.Snapshot()
	if !ok {
		t.Fatalf("expected snapshot")
	}
	byID := map[string]WorkflowNodeSnapshot{}
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}
	if !byID["root"].Completed {
		t.Fatalf("expected recovered root node to be completed")
	}
	if !byID["left"].Enqueued || !byID["right"].Enqueued {
		t.Fatalf("expected unlocked children to be enqueued: %#v", byID)
	}
}

func TestWorkflowManagerPriorityAwareModeOrdersReadyNodes(t *testing.T) {
	t.Parallel()

	manager := NewWorkflowManager()
	manager.SetTopologyMode(TopologyModePriorityAware)
	result, jobs, err := manager.LoadWorkflow(WorkflowSpec{
		ID: "wf-priority-test",
		Nodes: []WorkflowNode{
			{ID: "low", Priority: 1, WasmURL: "/low.wasm", RewardUSDC: "0.01"},
			{ID: "high", Priority: 10, WasmURL: "/high.wasm", RewardUSDC: "0.01"},
		},
	})
	if err != nil {
		t.Fatalf("LoadWorkflow() error = %v", err)
	}

	if !reflect.DeepEqual(result.TopologicalOrder, []string{"high", "low"}) {
		t.Fatalf("expected priority-aware topo order, got %#v", result.TopologicalOrder)
	}
	if len(jobs) != 2 || jobs[0].NodeID != "high" || jobs[1].NodeID != "low" {
		t.Fatalf("expected ready jobs to follow priority order, got %#v", jobs)
	}
}

func TestWorkflowManagerLoadsExpandedShardsAsIndependentJobs(t *testing.T) {
	t.Parallel()

	manager := NewWorkflowManager()
	result, jobs, err := manager.LoadWorkflow(WorkflowSpec{
		ID: "wf-sharded-runtime-test",
		Nodes: []WorkflowNode{
			{
				ID:                "prime-shard",
				WasmURL:           "/prime.wasm",
				Args:              []any{map[string]any{"range_size": 1000}},
				ShardCount:        3,
				ReplicationFactor: 3,
				AcceptancePolicy:  AcceptancePolicyConsensus,
				RewardUSDC:        "0.02",
			},
			{
				ID:         "reduce",
				DependsOn:  []string{"prime-shard"},
				WasmURL:    "/reduce.wasm",
				RewardUSDC: "0.01",
			},
		},
	})
	if err != nil {
		t.Fatalf("LoadWorkflow() error = %v", err)
	}

	wantReady := []string{"prime-shard-0", "prime-shard-1", "prime-shard-2"}
	if !reflect.DeepEqual(result.EnqueuedNodes, wantReady) {
		t.Fatalf("expected expanded shards to enqueue first, got %#v", result.EnqueuedNodes)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected 3 shard jobs, got %#v", jobs)
	}
	for i, job := range jobs {
		if job.NodeID != wantReady[i] || job.ReplicationFactor != 3 || job.AcceptancePolicy != AcceptancePolicyConsensus {
			t.Fatalf("unexpected shard job %d: %#v", i, job)
		}
		args, ok := job.Args[0].(map[string]any)
		if !ok || args["shard_index"] != i {
			t.Fatalf("expected shard_index %d in job args, got %#v", i, job.Args)
		}
	}

	next, err := manager.OnJobFinalized("wf-sharded-runtime-test:prime-shard-0", map[string]any{"output": []any{}})
	if err != nil {
		t.Fatalf("OnJobFinalized(first shard) error = %v", err)
	}
	if len(next) != 0 {
		t.Fatalf("reducer should wait for all shards, got %#v", next)
	}
	if _, err := manager.OnJobFinalized("wf-sharded-runtime-test:prime-shard-1", map[string]any{"output": []any{}}); err != nil {
		t.Fatalf("OnJobFinalized(second shard) error = %v", err)
	}
	next, err = manager.OnJobFinalized("wf-sharded-runtime-test:prime-shard-2", map[string]any{"output": []any{}})
	if err != nil {
		t.Fatalf("OnJobFinalized(third shard) error = %v", err)
	}
	if len(next) != 1 || next[0].NodeID != "reduce" {
		t.Fatalf("expected reducer after all shards finalize, got %#v", next)
	}
	gotDeps := make([]string, 0, len(next[0].Dependencies))
	for _, dep := range next[0].Dependencies {
		gotDeps = append(gotDeps, dep.NodeID)
	}
	if !reflect.DeepEqual(gotDeps, wantReady) {
		t.Fatalf("expected reducer deps %#v, got %#v", wantReady, gotDeps)
	}
}

func TestWorkflowManagerFinalizationWaitsForAllParents(t *testing.T) {
	t.Parallel()

	manager := NewWorkflowManager()
	_, jobs, err := manager.LoadWorkflow(WorkflowSpec{
		ID: "wf-join-test",
		Nodes: []WorkflowNode{
			{ID: "root-a", WasmURL: "/a.wasm", RewardUSDC: "0.01"},
			{ID: "root-b", WasmURL: "/b.wasm", RewardUSDC: "0.01"},
			{ID: "join", DependsOn: []string{"root-a", "root-b"}, WasmURL: "/join.wasm", RewardUSDC: "0.01"},
		},
	})
	if err != nil {
		t.Fatalf("LoadWorkflow() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected two root jobs, got %#v", jobs)
	}

	next, err := manager.OnJobFinalized("wf-join-test:root-a", map[string]any{"output": "a"})
	if err != nil {
		t.Fatalf("OnJobFinalized(root-a) error = %v", err)
	}
	if len(next) != 0 {
		t.Fatalf("join node should wait for root-b, got %#v", next)
	}

	next, err = manager.OnJobFinalized("wf-join-test:root-b", map[string]any{"output": "b"})
	if err != nil {
		t.Fatalf("OnJobFinalized(root-b) error = %v", err)
	}
	if len(next) != 1 || next[0].NodeID != "join" {
		t.Fatalf("expected join node after both parents finalize, got %#v", next)
	}
}
