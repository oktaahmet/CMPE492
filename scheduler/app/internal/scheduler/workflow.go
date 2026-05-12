package scheduler

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

// WorkflowSpec is the persisted/user-authored workflow document. It describes a
// DAG of compute nodes plus optional input artifacts shared by those nodes.
type WorkflowSpec struct {
	ID        string             `json:"id"`
	Artifacts []WorkflowArtifact `json:"artifacts,omitempty"`
	Nodes     []WorkflowNode     `json:"nodes"`
}

// WorkflowNode is one DAG vertex. The scheduler turns ready nodes into Jobs;
// the engine handles assignment/replication after that handoff.
type WorkflowNode struct {
	ID                string                      `json:"id"`
	DependsOn         []string                    `json:"depends_on,omitempty"`
	Priority          int                         `json:"priority,omitempty"`
	Program           string                      `json:"program,omitempty"`
	WasmURL           string                      `json:"wasm_url"`
	ExecutionTarget   ExecutionTarget             `json:"execution_target,omitempty"`
	Args              []any                       `json:"args,omitempty"`
	ShardCount        int                         `json:"shard_count,omitempty"`
	UsesArtifacts     []string                    `json:"uses_artifacts,omitempty"`
	OutputArtifacts   []WorkflowArtifact          `json:"output_artifacts,omitempty"`
	ResultSchema      map[string]PayloadFieldRule `json:"result_schema,omitempty"`
	RewardUSDC        string                      `json:"reward_usdc"`
	ReplicationFactor int                         `json:"replication_factor,omitempty"`
	AcceptancePolicy  AcceptancePolicy            `json:"acceptance_policy,omitempty"`
	Traits            []string                    `json:"traits,omitempty"`
}

const maxWorkflowShardCount = 10000

// WorkflowLoadResult reports what the manager accepted and which initial nodes
// became runnable after validation/recovery.
type WorkflowLoadResult struct {
	WorkflowID       string   `json:"workflow_id"`
	TopologicalOrder []string `json:"topological_order"`
	EnqueuedNodes    []string `json:"enqueued_nodes"`
	EnqueuedJobIDs   []string `json:"enqueued_job_ids"`
}

type TopologyMode string

const (
	TopologyModePlain         TopologyMode = "plain"
	TopologyModePriorityAware TopologyMode = "priority_aware"
)

// workflowRuntime is the in-memory DAG state for the currently loaded workflow.
// pendingDeps counts unfinished parents; dependents is the reverse adjacency map
// used to unlock children when a node finalizes.
type workflowRuntime struct {
	spec      WorkflowSpec
	nodesByID map[string]WorkflowNode
	topo      []string
	// outputs is the completed-node set. Persisted outputs are replayed here on
	// load so an interrupted workflow can continue from the first unfinished node.
	outputs     map[string]map[string]any
	enqueued    map[string]bool
	pendingDeps map[string]int
	dependents  map[string][]string
}

// WorkflowManager owns DAG progression. It does not execute jobs itself; it
// converts ready workflow nodes to scheduler Jobs and tracks job_id -> node_id.
type WorkflowManager struct {
	mu        sync.Mutex
	mode      TopologyMode
	runtime   *workflowRuntime
	jobToNode map[string]string
}

// WorkflowNodeSnapshot is the UI/runtime view of a node. It intentionally
// exposes metadata and state, not stored output bodies.
type WorkflowNodeSnapshot struct {
	ID                string             `json:"id"`
	DependsOn         []string           `json:"depends_on,omitempty"`
	Priority          int                `json:"priority,omitempty"`
	Program           string             `json:"program,omitempty"`
	WasmURL           string             `json:"wasm_url"`
	ExecutionTarget   ExecutionTarget    `json:"execution_target,omitempty"`
	RewardUSDC        string             `json:"reward_usdc"`
	Completed         bool               `json:"completed"`
	Enqueued          bool               `json:"enqueued"`
	UsesArtifacts     []string           `json:"uses_artifacts,omitempty"`
	OutputArtifacts   []WorkflowArtifact `json:"output_artifacts,omitempty"`
	ReplicationFactor int                `json:"replication_factor,omitempty"`
	AcceptancePolicy  AcceptancePolicy   `json:"acceptance_policy,omitempty"`
	Traits            []string           `json:"traits,omitempty"`
}

// WorkflowRuntimeSnapshot is the live UI/runtime graph payload.
type WorkflowRuntimeSnapshot struct {
	WorkflowID       string                 `json:"workflow_id"`
	TopologicalOrder []string               `json:"topological_order"`
	Nodes            []WorkflowNodeSnapshot `json:"nodes"`
}

// NewWorkflowManager creates an empty manager using plain lexicographic
// scheduling for equally-ready nodes.
func NewWorkflowManager() *WorkflowManager {
	return &WorkflowManager{
		mode:      TopologyModePlain,
		jobToNode: make(map[string]string),
	}
}

// NormalizeTopologyMode maps unknown/empty values to the default mode.
func NormalizeTopologyMode(raw string) TopologyMode {
	switch TopologyMode(strings.TrimSpace(raw)) {
	case TopologyModePriorityAware:
		return TopologyModePriorityAware
	default:
		return TopologyModePlain
	}
}

// IsValidTopologyMode reports whether a requested topology mode can be stored.
func IsValidTopologyMode(raw string) bool {
	switch TopologyMode(strings.TrimSpace(raw)) {
	case TopologyModePlain, TopologyModePriorityAware:
		return true
	default:
		return false
	}
}

// SetTopologyMode changes the ordering strategy used when multiple nodes are
// ready at the same time.
func (m *WorkflowManager) SetTopologyMode(mode TopologyMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = NormalizeTopologyMode(string(mode))
}

// TopologyMode returns the manager's current ready-node ordering mode.
func (m *WorkflowManager) TopologyMode() TopologyMode {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mode
}

// ValidateWorkflowSpec normalizes and validates a workflow without loading it.
func ValidateWorkflowSpec(spec WorkflowSpec) (WorkflowSpec, error) {
	normalized, _, _, err := normalizeAndValidateWorkflow(spec, TopologyModePlain)
	if err != nil {
		return WorkflowSpec{}, err
	}
	return normalized, nil
}

// LoadWorkflow validates a fresh workflow and enqueues its initial ready nodes.
func (m *WorkflowManager) LoadWorkflow(spec WorkflowSpec) (WorkflowLoadResult, []Job, error) {
	return m.LoadWorkflowWithCompleted(spec, nil)
}

// LoadWorkflowWithCompleted validates a workflow, replays previously finalized
// node outputs, and returns the nodes that are ready to execute next.
func (m *WorkflowManager) LoadWorkflowWithCompleted(spec WorkflowSpec, completed map[string]map[string]any) (WorkflowLoadResult, []Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.runtime != nil {
		return WorkflowLoadResult{}, nil, fmt.Errorf("workflow already exists: %s", m.runtime.spec.ID)
	}

	normalized, nodesByID, topo, err := normalizeAndValidateWorkflow(spec, m.mode)
	if err != nil {
		return WorkflowLoadResult{}, nil, err
	}

	runtime := &workflowRuntime{
		spec:        normalized,
		nodesByID:   nodesByID,
		topo:        topo,
		outputs:     map[string]map[string]any{},
		enqueued:    map[string]bool{},
		pendingDeps: map[string]int{},
		dependents:  map[string][]string{},
	}
	initializeWorkflowRuntime(runtime)

	// Recovered completions update dependency counters before ready nodes are
	// selected; this is what lets activation resume a partially finished DAG.
	for nodeID, output := range completed {
		if _, exists := runtime.nodesByID[nodeID]; !exists {
			continue
		}
		markNodeCompletedLocked(runtime, nodeID, output)
	}

	// After recovery, any node with all parents completed and not already
	// completed becomes an initial job. Root nodes naturally fall into this set.
	ready := readyNodesLocked(runtime, runtime.topo, m.mode)
	jobs := make([]Job, 0, len(ready))
	jobIDs := make([]string, 0, len(ready))
	for _, nodeID := range ready {
		node := runtime.nodesByID[nodeID]
		job := jobFromNode(runtime, node)
		jobs = append(jobs, job)
		jobIDs = append(jobIDs, job.ID)
		runtime.enqueued[nodeID] = true
		m.jobToNode[job.ID] = nodeID
	}
	m.runtime = runtime

	return WorkflowLoadResult{
		WorkflowID:       normalized.ID,
		TopologicalOrder: append([]string(nil), topo...),
		EnqueuedNodes:    append([]string(nil), ready...),
		EnqueuedJobIDs:   jobIDs,
	}, jobs, nil
}

// ActiveWorkflowID returns the loaded workflow id, or empty when no workflow is
// currently active in memory.
func (m *WorkflowManager) ActiveWorkflowID() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.runtime == nil {
		return ""
	}
	return m.runtime.spec.ID
}

// ClearWorkflow forgets the loaded DAG and all job-to-node routing state.
func (m *WorkflowManager) ClearWorkflow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtime = nil
	m.jobToNode = make(map[string]string)
}

// Snapshot returns the current runtime graph for the live UI.
func (m *WorkflowManager) Snapshot() (WorkflowRuntimeSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtime
	if runtime == nil {
		return WorkflowRuntimeSnapshot{}, false
	}

	nodes := make([]WorkflowNodeSnapshot, 0, len(runtime.topo))
	for _, nodeID := range runtime.topo {
		node := runtime.nodesByID[nodeID]
		nodes = append(nodes, WorkflowNodeSnapshot{
			ID:                node.ID,
			DependsOn:         append([]string(nil), node.DependsOn...),
			Priority:          node.Priority,
			Program:           node.Program,
			WasmURL:           node.WasmURL,
			ExecutionTarget:   node.ExecutionTarget,
			RewardUSDC:        node.RewardUSDC,
			Completed:         isNodeCompleted(runtime, nodeID),
			Enqueued:          runtime.enqueued[nodeID],
			UsesArtifacts:     append([]string(nil), node.UsesArtifacts...),
			OutputArtifacts:   cloneArtifacts(node.OutputArtifacts),
			ReplicationFactor: node.ReplicationFactor,
			AcceptancePolicy:  node.AcceptancePolicy,
			Traits:            append([]string(nil), node.Traits...),
		})
	}

	return WorkflowRuntimeSnapshot{
		WorkflowID:       runtime.spec.ID,
		TopologicalOrder: append([]string(nil), runtime.topo...),
		Nodes:            nodes,
	}, true
}

// OnJobFinalized records a finalized job output and returns newly-unlocked child
// jobs. Unknown/stale job IDs are ignored so duplicate finalized callbacks stay
// harmless.
func (m *WorkflowManager) OnJobFinalized(jobID string, output map[string]any) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nodeID, ok := m.jobToNode[jobID]
	if !ok {
		return nil, nil
	}
	runtime := m.runtime
	if runtime == nil {
		delete(m.jobToNode, jobID)
		return nil, nil
	}

	markNodeCompletedLocked(runtime, nodeID, output)
	delete(m.jobToNode, jobID)

	// Only direct children can become newly ready after this node completes.
	// Their own pendingDeps counters already include all other prerequisites.
	ready := readyNodesLocked(runtime, runtime.dependents[nodeID], m.mode)
	if len(ready) == 0 {
		return nil, nil
	}

	nextJobs := make([]Job, 0, len(ready))
	for _, nodeID := range ready {
		node := runtime.nodesByID[nodeID]
		job := jobFromNode(runtime, node)
		nextJobs = append(nextJobs, job)
		runtime.enqueued[nodeID] = true
		m.jobToNode[job.ID] = nodeID
	}

	return nextJobs, nil
}

// initializeWorkflowRuntime builds the dependency counters and reverse edges
// used by the incremental unlock path.
func initializeWorkflowRuntime(runtime *workflowRuntime) {
	for _, nodeID := range runtime.topo {
		node := runtime.nodesByID[nodeID]
		runtime.pendingDeps[nodeID] = len(node.DependsOn)
		if _, exists := runtime.dependents[nodeID]; !exists {
			runtime.dependents[nodeID] = nil
		}
		for _, depID := range node.DependsOn {
			runtime.dependents[depID] = append(runtime.dependents[depID], nodeID)
		}
	}
}

// markNodeCompletedLocked stores a node output and decrements each direct
// child's pending dependency counter exactly once.
func markNodeCompletedLocked(runtime *workflowRuntime, nodeID string, output map[string]any) {
	if isNodeCompleted(runtime, nodeID) {
		return
	}

	runtime.outputs[nodeID] = cloneJSONMap(output)

	// pendingDeps never scans all parents. Each finalized parent decrements its
	// direct children; a child reaches zero only after all parents are complete.
	for _, childID := range runtime.dependents[nodeID] {
		if runtime.pendingDeps[childID] > 0 {
			runtime.pendingDeps[childID]--
		}
	}
}

// readyNodesLocked filters candidate nodes down to runnable nodes and sorts them
// according to the configured topology mode.
func readyNodesLocked(runtime *workflowRuntime, candidateIDs []string, mode TopologyMode) []string {
	ready := make([]string, 0, len(candidateIDs))
	for _, nodeID := range candidateIDs {
		if runtime.enqueued[nodeID] || isNodeCompleted(runtime, nodeID) {
			continue
		}
		if runtime.pendingDeps[nodeID] != 0 {
			continue
		}
		ready = append(ready, nodeID)
	}
	sortReadyNodeIDs(ready, runtime.nodesByID, mode)
	return ready
}

// jobFromNode converts workflow metadata into an Engine job assignment model.
func jobFromNode(runtime *workflowRuntime, node WorkflowNode) Job {
	args := append([]any(nil), node.Args...)

	deps := make([]DependencyRef, 0, len(node.DependsOn))
	for _, depID := range node.DependsOn {
		deps = append(deps, DependencyRef{
			WorkflowID: runtime.spec.ID,
			NodeID:     depID,
		})
	}
	artifactByID := make(map[string]WorkflowArtifact, len(runtime.spec.Artifacts))
	for _, artifact := range runtime.spec.Artifacts {
		artifactByID[artifact.ID] = artifact
	}
	// Jobs receive only the workflow artifacts they explicitly requested. The
	// manager keeps artifact hydration separate, so the engine only sees metadata.
	artifacts := make([]WorkflowArtifact, 0, len(node.UsesArtifacts))
	for _, artifactID := range node.UsesArtifacts {
		if artifact, ok := artifactByID[artifactID]; ok {
			artifacts = append(artifacts, artifact)
		}
	}

	return Job{
		ID:                jobID(runtime.spec.ID, node.ID),
		WorkflowID:        runtime.spec.ID,
		NodeID:            node.ID,
		WasmURL:           node.WasmURL,
		ExecutionTarget:   node.ExecutionTarget,
		Args:              args,
		Dependencies:      deps,
		Artifacts:         cloneArtifacts(artifacts),
		OutputArtifacts:   cloneArtifacts(node.OutputArtifacts),
		ResultSchema:      node.ResultSchema,
		RewardUSDC:        node.RewardUSDC,
		ReplicationFactor: node.ReplicationFactor,
		AcceptancePolicy:  node.AcceptancePolicy,
		Traits:            append([]string(nil), node.Traits...),
	}
}

// isNodeCompleted reports whether the runtime has an output for a node.
func isNodeCompleted(runtime *workflowRuntime, nodeID string) bool {
	if runtime == nil {
		return false
	}
	_, exists := runtime.outputs[nodeID]
	return exists
}

// jobID gives each node job a deterministic id inside its workflow.
func jobID(workflowID, nodeID string) string {
	return fmt.Sprintf("%s:%s", workflowID, nodeID)
}

// normalizeAndValidateWorkflow trims/defaults a workflow, checks DAG validity,
// and returns lookup structures used by the runtime.
func normalizeAndValidateWorkflow(spec WorkflowSpec, mode TopologyMode) (WorkflowSpec, map[string]WorkflowNode, []string, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		return WorkflowSpec{}, nil, nil, errors.New("workflow id is required")
	}
	if len(spec.Nodes) == 0 {
		return WorkflowSpec{}, nil, nil, errors.New("workflow nodes are required")
	}
	var err error
	spec, err = expandShardedNodes(spec)
	if err != nil {
		return WorkflowSpec{}, nil, nil, err
	}
	artifacts, artifactIDs, err := normalizeArtifacts(spec.Artifacts, "data")
	if err != nil {
		return WorkflowSpec{}, nil, nil, err
	}

	nodesByID := make(map[string]WorkflowNode, len(spec.Nodes))
	normalizedNodes := make([]WorkflowNode, 0, len(spec.Nodes))
	for _, raw := range spec.Nodes {
		node := raw
		node.ID = strings.TrimSpace(node.ID)
		node.Program = strings.TrimSpace(strings.ReplaceAll(node.Program, "\\", "/"))
		node.WasmURL = strings.TrimSpace(node.WasmURL)
		node.RewardUSDC = strings.TrimSpace(node.RewardUSDC)
		rawAcceptancePolicy := strings.TrimSpace(string(node.AcceptancePolicy))
		rawExecutionTarget := strings.TrimSpace(string(node.ExecutionTarget))
		node.Traits = normalizeTraits(node.Traits)
		node.UsesArtifacts = normalizeStringList(node.UsesArtifacts)
		outputArtifacts, outputArtifactIDs, err := normalizeArtifacts(node.OutputArtifacts, "")
		if err != nil {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s output_artifacts invalid: %w", node.ID, err)
		}
		node.OutputArtifacts = outputArtifacts

		if node.ID == "" {
			return WorkflowSpec{}, nil, nil, errors.New("node id is required")
		}
		if node.WasmURL == "" {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("wasm_url is required for node %s", node.ID)
		}
		if !IsValidExecutionTarget(rawExecutionTarget) {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s execution_target is invalid", node.ID)
		}
		node.ExecutionTarget = NormalizeExecutionTarget(rawExecutionTarget)
		if node.RewardUSDC == "" {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("reward_usdc is required for node %s", node.ID)
		}
		if node.ReplicationFactor < 0 {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s replication_factor must be >= 0", node.ID)
		}
		if node.ExecutionTarget == ExecutionTargetServer && node.ReplicationFactor > 1 {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s server execution_target requires replication_factor <= 1", node.ID)
		}
		if node.ExecutionTarget != ExecutionTargetServer && len(node.OutputArtifacts) > 0 {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s output_artifacts are currently supported only for server execution_target", node.ID)
		}
		if !IsValidAcceptancePolicy(rawAcceptancePolicy) {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s acceptance_policy is invalid", node.ID)
		}
		node.AcceptancePolicy = NormalizeAcceptancePolicy(rawAcceptancePolicy)
		if len(outputArtifactIDs) != len(node.OutputArtifacts) {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s output_artifacts contains duplicate ids", node.ID)
		}
		if _, exists := nodesByID[node.ID]; exists {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("duplicate node id: %s", node.ID)
		}
		for _, artifactID := range node.UsesArtifacts {
			if !artifactIDs[artifactID] {
				return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s uses unknown artifact %s", node.ID, artifactID)
			}
		}

		// Dependencies are normalized before the unknown-node check below so
		// duplicate parent names do not inflate pendingDeps.
		seenDeps := map[string]bool{}
		deps := make([]string, 0, len(node.DependsOn))
		for _, dep := range node.DependsOn {
			trimmed := strings.TrimSpace(dep)
			if trimmed == "" {
				continue
			}
			if trimmed == node.ID {
				return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s cannot depend on itself", node.ID)
			}
			if seenDeps[trimmed] {
				continue
			}
			seenDeps[trimmed] = true
			deps = append(deps, trimmed)
		}
		sort.Strings(deps)
		node.DependsOn = deps

		if err := validateNodeResultSchema(node.ID, node.ResultSchema); err != nil {
			return WorkflowSpec{}, nil, nil, err
		}

		nodesByID[node.ID] = node
		normalizedNodes = append(normalizedNodes, node)
	}

	// Unknown dependencies are checked after all nodes are collected so forward
	// references are allowed in workflow JSON.
	for _, node := range normalizedNodes {
		for _, dep := range node.DependsOn {
			if _, ok := nodesByID[dep]; !ok {
				return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s depends on unknown node %s", node.ID, dep)
			}
		}
	}

	topo, err := topologicalSort(nodesByID, mode)
	if err != nil {
		return WorkflowSpec{}, nil, nil, err
	}

	return WorkflowSpec{
		ID:        spec.ID,
		Artifacts: artifacts,
		Nodes:     normalizedNodes,
	}, nodesByID, topo, nil
}

// normalizeArtifacts validates artifact ids/files/paths and returns both sorted
// metadata and an id set for uses_artifacts checks. New authoring should use
// file; path remains supported for older workflow JSON.
func normalizeArtifacts(raw []WorkflowArtifact, fileDefaultDir string) ([]WorkflowArtifact, map[string]bool, error) {
	seen := make(map[string]bool, len(raw))
	out := make([]WorkflowArtifact, 0, len(raw))
	for _, artifact := range raw {
		artifact.ID = strings.TrimSpace(artifact.ID)
		artifact.File = strings.TrimSpace(strings.ReplaceAll(artifact.File, "\\", "/"))
		artifact.Path = strings.TrimSpace(strings.ReplaceAll(artifact.Path, "\\", "/"))
		artifact.URL = strings.TrimSpace(artifact.URL)
		artifact.SHA256 = strings.TrimSpace(artifact.SHA256)
		artifact.ContentType = strings.TrimSpace(artifact.ContentType)
		if artifact.ID == "" {
			return nil, nil, errors.New("artifact id is required")
		}
		if !isSafeArtifactID(artifact.ID) {
			return nil, nil, fmt.Errorf("artifact id is invalid: %s", artifact.ID)
		}
		if seen[artifact.ID] {
			return nil, nil, fmt.Errorf("duplicate artifact id: %s", artifact.ID)
		}
		if artifact.Path == "" && artifact.File != "" {
			if !isSafeArtifactFile(artifact.File) {
				return nil, nil, fmt.Errorf("artifact %s file must be a filename", artifact.ID)
			}
			if fileDefaultDir != "" {
				artifact.Path = path.Join(fileDefaultDir, artifact.File)
			} else {
				artifact.Path = artifact.File
			}
		}
		if artifact.Path == "" {
			return nil, nil, fmt.Errorf("artifact %s file is required", artifact.ID)
		}
		// Artifact paths are workflow-relative. This prevents uploaded workflow
		// specs from reading files outside their own directory.
		cleaned := path.Clean(artifact.Path)
		if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || path.IsAbs(cleaned) {
			return nil, nil, fmt.Errorf("artifact %s path must be relative and stay inside workflow folder", artifact.ID)
		}
		artifact.Path = cleaned
		if artifact.File == "" && !strings.Contains(cleaned, "/") {
			artifact.File = cleaned
		}
		seen[artifact.ID] = true
		out = append(out, artifact)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, seen, nil
}

func isSafeArtifactFile(value string) bool {
	if value == "" || strings.Contains(value, "/") || value == "." || value == ".." {
		return false
	}
	return path.Clean(value) == value
}

// isSafeArtifactID restricts artifact ids to simple stable path/query-safe
// tokens.
func isSafeArtifactID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func expandShardedNodes(spec WorkflowSpec) (WorkflowSpec, error) {
	authoredIDs := make(map[string]bool, len(spec.Nodes))
	for _, node := range spec.Nodes {
		id := strings.TrimSpace(node.ID)
		if id == "" {
			continue
		}
		if authoredIDs[id] {
			return WorkflowSpec{}, fmt.Errorf("duplicate node id: %s", id)
		}
		authoredIDs[id] = true
	}

	childrenByTemplateID := make(map[string][]string)
	expanded := make([]WorkflowNode, 0, len(spec.Nodes))
	for _, raw := range spec.Nodes {
		nodeID := strings.TrimSpace(raw.ID)
		if raw.ShardCount < 0 {
			return WorkflowSpec{}, fmt.Errorf("node %s shard_count must be >= 0", nodeID)
		}
		if raw.ShardCount == 0 {
			expanded = append(expanded, raw)
			continue
		}
		if raw.ShardCount > maxWorkflowShardCount {
			return WorkflowSpec{}, fmt.Errorf("node %s shard_count must be <= %d", nodeID, maxWorkflowShardCount)
		}
		if nodeID == "" {
			return WorkflowSpec{}, errors.New("node id is required")
		}

		children := make([]string, 0, raw.ShardCount)
		for shardIndex := 0; shardIndex < raw.ShardCount; shardIndex++ {
			child := raw
			child.ID = fmt.Sprintf("%s-%d", nodeID, shardIndex)
			child.ShardCount = 0

			args, err := injectShardIndex(child.Args, shardIndex)
			if err != nil {
				return WorkflowSpec{}, fmt.Errorf("node %s args invalid: %w", nodeID, err)
			}
			child.Args = args

			expanded = append(expanded, child)
			children = append(children, child.ID)
		}
		childrenByTemplateID[nodeID] = children
	}

	for i := range expanded {
		if len(expanded[i].DependsOn) == 0 {
			continue
		}
		deps := make([]string, 0, len(expanded[i].DependsOn))
		for _, dep := range expanded[i].DependsOn {
			trimmed := strings.TrimSpace(dep)
			if trimmed == "" {
				continue
			}
			if children, ok := childrenByTemplateID[trimmed]; ok {
				deps = append(deps, children...)
				continue
			}
			deps = append(deps, trimmed)
		}
		expanded[i].DependsOn = deps
	}

	spec.Nodes = expanded
	return spec, nil
}

func injectShardIndex(args []any, shardIndex int) ([]any, error) {
	if len(args) == 0 {
		return []any{map[string]any{"shard_index": shardIndex}}, nil
	}

	out := make([]any, len(args))
	for i, arg := range args {
		out[i] = cloneValue(arg)
	}

	first, ok := out[0].(map[string]any)
	if !ok {
		return nil, errors.New("first args item must be an object when shard_count is set")
	}
	first["shard_index"] = shardIndex
	return out, nil
}

// normalizeStringList trims, deduplicates, and sorts user-authored string lists
// while preserving case.
func normalizeStringList(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloneArtifacts protects runtime state from callers mutating slices returned
// through jobs or snapshots.
func cloneArtifacts(in []WorkflowArtifact) []WorkflowArtifact {
	if len(in) == 0 {
		return nil
	}
	out := make([]WorkflowArtifact, len(in))
	copy(out, in)
	return out
}

// normalizeTraits turns descriptive node tags into stable lowercase metadata for
// UI display and future capability matching.
func normalizeTraits(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(raw))
	traits := make([]string, 0, len(raw))
	for _, trait := range raw {
		trimmed := strings.ToLower(strings.TrimSpace(trait))
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		traits = append(traits, trimmed)
	}
	sort.Strings(traits)
	if len(traits) == 0 {
		return nil
	}
	return traits
}

// topologicalSort returns a deterministic Kahn topological ordering and rejects
// cyclic workflows.
func topologicalSort(nodesByID map[string]WorkflowNode, mode TopologyMode) ([]string, error) {
	inDegree := make(map[string]int, len(nodesByID))
	edges := make(map[string][]string, len(nodesByID))

	for id := range nodesByID {
		inDegree[id] = 0
		edges[id] = []string{}
	}

	for _, node := range nodesByID {
		for _, dep := range node.DependsOn {
			edges[dep] = append(edges[dep], node.ID)
			inDegree[node.ID]++
		}
	}

	// ready is repeatedly sorted so both the final topo order and each ready
	// batch are deterministic across map iteration order.
	for id := range edges {
		sort.Strings(edges[id])
	}

	ready := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	sortReadyNodeIDs(ready, nodesByID, mode)

	out := make([]string, 0, len(nodesByID))
	for len(ready) > 0 {
		current := ready[0]
		ready = ready[1:]
		out = append(out, current)

		for _, child := range edges[current] {
			inDegree[child]--
			if inDegree[child] == 0 {
				ready = append(ready, child)
			}
		}
		sortReadyNodeIDs(ready, nodesByID, mode)
	}

	if len(out) != len(nodesByID) {
		return nil, errors.New("workflow graph contains cycle")
	}
	return out, nil
}

// sortReadyNodeIDs orders runnable nodes. Priority-aware mode prefers larger
// priority values, then falls back to node id for deterministic ties.
func sortReadyNodeIDs(ids []string, nodesByID map[string]WorkflowNode, mode TopologyMode) {
	sort.Slice(ids, func(i, j int) bool {
		if mode != TopologyModePriorityAware {
			return ids[i] < ids[j]
		}
		left := nodesByID[ids[i]]
		right := nodesByID[ids[j]]
		if left.Priority == right.Priority {
			return ids[i] < ids[j]
		}
		return left.Priority > right.Priority
	})
}

// validateNodeResultSchema rejects schema field types the engine cannot enforce.
func validateNodeResultSchema(nodeID string, schema map[string]PayloadFieldRule) error {
	for field, rule := range schema {
		expected := strings.ToLower(strings.TrimSpace(rule.Type))
		switch expected {
		case "string", "number", "boolean", "bool", "object", "array", "null":
		default:
			return fmt.Errorf("node %s result_schema.%s has unsupported type %q", nodeID, field, rule.Type)
		}
	}
	return nil
}
