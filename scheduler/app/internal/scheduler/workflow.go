package scheduler

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

type WorkflowSpec struct {
	ID        string             `json:"id"`
	Artifacts []WorkflowArtifact `json:"artifacts,omitempty"`
	Nodes     []WorkflowNode     `json:"nodes"`
}

type WorkflowNode struct {
	ID                string                      `json:"id"`
	DependsOn         []string                    `json:"depends_on,omitempty"`
	Priority          int                         `json:"priority,omitempty"`
	WasmURL           string                      `json:"wasm_url"`
	ExecutionTarget   ExecutionTarget             `json:"execution_target,omitempty"`
	Args              []any                       `json:"args,omitempty"`
	UsesArtifacts     []string                    `json:"uses_artifacts,omitempty"`
	OutputArtifacts   []WorkflowArtifact          `json:"output_artifacts,omitempty"`
	ResultSchema      map[string]PayloadFieldRule `json:"result_schema,omitempty"`
	RewardUSDC        string                      `json:"reward_usdc"`
	ReplicationFactor int                         `json:"replication_factor,omitempty"`
	AcceptancePolicy  AcceptancePolicy            `json:"acceptance_policy,omitempty"`
	Traits            []string                    `json:"traits,omitempty"`
}

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

type workflowRuntime struct {
	spec        WorkflowSpec
	nodesByID   map[string]WorkflowNode
	topo        []string
	outputs     map[string]map[string]any
	enqueued    map[string]bool
	pendingDeps map[string]int
	dependents  map[string][]string
}

type WorkflowManager struct {
	mu        sync.Mutex
	mode      TopologyMode
	runtime   *workflowRuntime
	jobToNode map[string]string
}

type WorkflowNodeSnapshot struct {
	ID                string             `json:"id"`
	DependsOn         []string           `json:"depends_on,omitempty"`
	Priority          int                `json:"priority,omitempty"`
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

type WorkflowRuntimeSnapshot struct {
	WorkflowID       string                 `json:"workflow_id"`
	TopologicalOrder []string               `json:"topological_order"`
	Nodes            []WorkflowNodeSnapshot `json:"nodes"`
}

func NewWorkflowManager() *WorkflowManager {
	return &WorkflowManager{
		mode:      TopologyModePlain,
		jobToNode: make(map[string]string),
	}
}

func NormalizeTopologyMode(raw string) TopologyMode {
	switch TopologyMode(strings.TrimSpace(raw)) {
	case TopologyModePriorityAware:
		return TopologyModePriorityAware
	default:
		return TopologyModePlain
	}
}

func IsValidTopologyMode(raw string) bool {
	switch TopologyMode(strings.TrimSpace(raw)) {
	case TopologyModePlain, TopologyModePriorityAware:
		return true
	default:
		return false
	}
}

func (m *WorkflowManager) SetTopologyMode(mode TopologyMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = NormalizeTopologyMode(string(mode))
}

func (m *WorkflowManager) TopologyMode() TopologyMode {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mode
}

func ValidateWorkflowSpec(spec WorkflowSpec) (WorkflowSpec, error) {
	normalized, _, _, err := normalizeAndValidateWorkflow(spec, TopologyModePlain)
	if err != nil {
		return WorkflowSpec{}, err
	}
	return normalized, nil
}

func (m *WorkflowManager) LoadWorkflow(spec WorkflowSpec) (WorkflowLoadResult, []Job, error) {
	return m.LoadWorkflowWithCompleted(spec, nil)
}

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

	for nodeID, output := range completed {
		if _, exists := runtime.nodesByID[nodeID]; !exists {
			continue
		}
		markNodeCompletedLocked(runtime, nodeID, output)
	}

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

func (m *WorkflowManager) ActiveWorkflowID() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.runtime == nil {
		return ""
	}
	return m.runtime.spec.ID
}

func (m *WorkflowManager) ClearWorkflow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtime = nil
	m.jobToNode = make(map[string]string)
}

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

func markNodeCompletedLocked(runtime *workflowRuntime, nodeID string, output map[string]any) {
	if isNodeCompleted(runtime, nodeID) {
		return
	}

	runtime.outputs[nodeID] = cloneJSONMap(output)

	for _, childID := range runtime.dependents[nodeID] {
		if runtime.pendingDeps[childID] > 0 {
			runtime.pendingDeps[childID]--
		}
	}
}

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

func isNodeCompleted(runtime *workflowRuntime, nodeID string) bool {
	if runtime == nil {
		return false
	}
	_, exists := runtime.outputs[nodeID]
	return exists
}

func jobID(workflowID, nodeID string) string {
	return fmt.Sprintf("%s:%s", workflowID, nodeID)
}

func normalizeAndValidateWorkflow(spec WorkflowSpec, mode TopologyMode) (WorkflowSpec, map[string]WorkflowNode, []string, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		return WorkflowSpec{}, nil, nil, errors.New("workflow id is required")
	}
	if len(spec.Nodes) == 0 {
		return WorkflowSpec{}, nil, nil, errors.New("workflow nodes are required")
	}
	artifacts, artifactIDs, err := normalizeArtifacts(spec.Artifacts)
	if err != nil {
		return WorkflowSpec{}, nil, nil, err
	}

	nodesByID := make(map[string]WorkflowNode, len(spec.Nodes))
	normalizedNodes := make([]WorkflowNode, 0, len(spec.Nodes))
	for _, raw := range spec.Nodes {
		node := raw
		node.ID = strings.TrimSpace(node.ID)
		node.WasmURL = strings.TrimSpace(node.WasmURL)
		node.RewardUSDC = strings.TrimSpace(node.RewardUSDC)
		node.AcceptancePolicy = NormalizeAcceptancePolicy(strings.TrimSpace(string(node.AcceptancePolicy)))
		node.ExecutionTarget = NormalizeExecutionTarget(strings.TrimSpace(string(node.ExecutionTarget)))
		node.Traits = normalizeTraits(node.Traits)
		node.UsesArtifacts = normalizeStringList(node.UsesArtifacts)
		outputArtifacts, outputArtifactIDs, err := normalizeArtifacts(node.OutputArtifacts)
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
		if !IsValidExecutionTarget(string(node.ExecutionTarget)) {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s execution_target is invalid", node.ID)
		}
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
		if !IsValidAcceptancePolicy(string(node.AcceptancePolicy)) {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("node %s acceptance_policy is invalid", node.ID)
		}
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

func normalizeArtifacts(raw []WorkflowArtifact) ([]WorkflowArtifact, map[string]bool, error) {
	seen := make(map[string]bool, len(raw))
	out := make([]WorkflowArtifact, 0, len(raw))
	for _, artifact := range raw {
		artifact.ID = strings.TrimSpace(artifact.ID)
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
		if artifact.Path == "" {
			return nil, nil, fmt.Errorf("artifact %s path is required", artifact.ID)
		}
		cleaned := path.Clean(artifact.Path)
		if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || path.IsAbs(cleaned) {
			return nil, nil, fmt.Errorf("artifact %s path must be relative and stay inside workflow folder", artifact.ID)
		}
		artifact.Path = cleaned
		seen[artifact.ID] = true
		out = append(out, artifact)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, seen, nil
}

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

func cloneArtifacts(in []WorkflowArtifact) []WorkflowArtifact {
	if len(in) == 0 {
		return nil
	}
	out := make([]WorkflowArtifact, len(in))
	copy(out, in)
	return out
}

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
