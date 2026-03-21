package scheduler

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type WorkflowSpec struct {
	ID    string         `json:"id"`
	Nodes []WorkflowNode `json:"nodes"`
}

type WorkflowNode struct {
	ID           string                      `json:"id"`
	DependsOn    []string                    `json:"depends_on,omitempty"`
	Priority     int                         `json:"priority,omitempty"`
	WasmURL      string                      `json:"wasm_url"`
	Args         []any                       `json:"args,omitempty"`
	ResultSchema map[string]PayloadFieldRule `json:"result_schema,omitempty"`
	RewardUSDC   string                      `json:"reward_usdc"`
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
	ID         string   `json:"id"`
	DependsOn  []string `json:"depends_on,omitempty"`
	Priority   int      `json:"priority,omitempty"`
	WasmURL    string   `json:"wasm_url"`
	RewardUSDC string   `json:"reward_usdc"`
	Completed  bool     `json:"completed"`
	Enqueued   bool     `json:"enqueued"`
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
			ID:         node.ID,
			DependsOn:  append([]string(nil), node.DependsOn...),
			Priority:   node.Priority,
			WasmURL:    node.WasmURL,
			RewardUSDC: node.RewardUSDC,
			Completed:  isNodeCompleted(runtime, nodeID),
			Enqueued:   runtime.enqueued[nodeID],
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
	args = appendDependencyScalarArgs(args, runtime.outputs, node.DependsOn)

	deps := make([]DependencyRef, 0, len(node.DependsOn))
	for _, depID := range node.DependsOn {
		deps = append(deps, DependencyRef{
			WorkflowID: runtime.spec.ID,
			NodeID:     depID,
		})
	}

	return Job{
		ID:           jobID(runtime.spec.ID, node.ID),
		WorkflowID:   runtime.spec.ID,
		NodeID:       node.ID,
		WasmURL:      node.WasmURL,
		Args:         args,
		Dependencies: deps,
		ResultSchema: node.ResultSchema,
		RewardUSDC:   node.RewardUSDC,
	}
}

func isNodeCompleted(runtime *workflowRuntime, nodeID string) bool {
	if runtime == nil {
		return false
	}
	_, exists := runtime.outputs[nodeID]
	return exists
}

func appendDependencyScalarArgs(
	args []any,
	completedOutputs map[string]map[string]any,
	dependsOn []string,
) []any {
	if len(dependsOn) == 0 {
		return args
	}

	out := append([]any(nil), args...)
	for _, depID := range dependsOn {
		payload := completedOutputs[depID]
		if payload == nil {
			continue
		}
		value, exists := payload["output"]
		if !exists {
			continue
		}
		if isScalarArg(value) {
			out = append(out, value)
		}
	}
	return out
}

func isScalarArg(value any) bool {
	switch value.(type) {
	case nil, bool, string,
		float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
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

	nodesByID := make(map[string]WorkflowNode, len(spec.Nodes))
	normalizedNodes := make([]WorkflowNode, 0, len(spec.Nodes))
	for _, raw := range spec.Nodes {
		node := raw
		node.ID = strings.TrimSpace(node.ID)
		node.WasmURL = strings.TrimSpace(node.WasmURL)
		node.RewardUSDC = strings.TrimSpace(node.RewardUSDC)

		if node.ID == "" {
			return WorkflowSpec{}, nil, nil, errors.New("node id is required")
		}
		if node.WasmURL == "" {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("wasm_url is required for node %s", node.ID)
		}
		if node.RewardUSDC == "" {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("reward_usdc is required for node %s", node.ID)
		}
		if _, exists := nodesByID[node.ID]; exists {
			return WorkflowSpec{}, nil, nil, fmt.Errorf("duplicate node id: %s", node.ID)
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
		ID:    spec.ID,
		Nodes: normalizedNodes,
	}, nodesByID, topo, nil
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
