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

type workflowRuntime struct {
	spec             WorkflowSpec
	nodesByID        map[string]WorkflowNode
	topo             []string
	completed        map[string]bool
	completedOutputs map[string]map[string]any
	enqueued         map[string]bool
}

type jobRef struct {
	workflowID string
	nodeID     string
}

type WorkflowManager struct {
	mu        sync.Mutex
	workflows map[string]*workflowRuntime
	jobToNode map[string]jobRef
}

func NewWorkflowManager() *WorkflowManager {
	return &WorkflowManager{
		workflows: make(map[string]*workflowRuntime),
		jobToNode: make(map[string]jobRef),
	}
}

func ValidateWorkflowSpec(spec WorkflowSpec) (WorkflowSpec, error) {
	normalized, _, _, err := normalizeAndValidateWorkflow(spec)
	if err != nil {
		return WorkflowSpec{}, err
	}
	return normalized, nil
}

func (m *WorkflowManager) LoadWorkflow(spec WorkflowSpec) (WorkflowLoadResult, []Job, error) {
	return m.LoadWorkflowWithCompleted(spec, nil)
}

func (m *WorkflowManager) LoadWorkflowWithCompleted(spec WorkflowSpec, completed map[string]map[string]any) (WorkflowLoadResult, []Job, error) {
	normalized, nodesByID, topo, err := normalizeAndValidateWorkflow(spec)
	if err != nil {
		return WorkflowLoadResult{}, nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.workflows[normalized.ID]; exists {
		return WorkflowLoadResult{}, nil, fmt.Errorf("workflow already exists: %s", normalized.ID)
	}

	runtime := &workflowRuntime{
		spec:             normalized,
		nodesByID:        nodesByID,
		topo:             topo,
		completed:        map[string]bool{},
		completedOutputs: map[string]map[string]any{},
		enqueued:         map[string]bool{},
	}

	for nodeID, output := range completed {
		if _, exists := runtime.nodesByID[nodeID]; !exists {
			continue
		}
		runtime.completed[nodeID] = true
		runtime.completedOutputs[nodeID] = cloneJSONMap(output)
	}

	ready := readyNodesLocked(runtime)
	jobs := make([]Job, 0, len(ready))
	jobIDs := make([]string, 0, len(ready))
	for _, nodeID := range ready {
		node := runtime.nodesByID[nodeID]
		job := jobFromNode(runtime, node)
		jobs = append(jobs, job)
		jobIDs = append(jobIDs, job.ID)
		runtime.enqueued[nodeID] = true
		m.jobToNode[job.ID] = jobRef{workflowID: normalized.ID, nodeID: nodeID}
	}

	m.workflows[normalized.ID] = runtime

	return WorkflowLoadResult{
		WorkflowID:       normalized.ID,
		TopologicalOrder: append([]string(nil), topo...),
		EnqueuedNodes:    append([]string(nil), ready...),
		EnqueuedJobIDs:   jobIDs,
	}, jobs, nil
}

func (m *WorkflowManager) DeleteWorkflow(workflowID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteWorkflowLocked(workflowID)
}

func (m *WorkflowManager) HasWorkflow(workflowID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.workflows[workflowID]
	return ok
}

func (m *WorkflowManager) WorkflowIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make([]string, 0, len(m.workflows))
	for id := range m.workflows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m *WorkflowManager) OnJobFinalized(jobID string, output map[string]any) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ref, ok := m.jobToNode[jobID]
	if !ok {
		return nil, nil
	}
	runtime := m.workflows[ref.workflowID]
	if runtime == nil {
		delete(m.jobToNode, jobID)
		return nil, nil
	}

	runtime.completed[ref.nodeID] = true
	runtime.completedOutputs[ref.nodeID] = cloneJSONMap(output)
	delete(m.jobToNode, jobID)

	ready := readyNodesLocked(runtime)
	if len(ready) == 0 {
		return nil, nil
	}

	nextJobs := make([]Job, 0, len(ready))
	for _, nodeID := range ready {
		node := runtime.nodesByID[nodeID]
		job := jobFromNode(runtime, node)
		nextJobs = append(nextJobs, job)
		runtime.enqueued[nodeID] = true
		m.jobToNode[job.ID] = jobRef{
			workflowID: runtime.spec.ID,
			nodeID:     nodeID,
		}
	}

	return nextJobs, nil
}

func (m *WorkflowManager) deleteWorkflowLocked(workflowID string) {
	delete(m.workflows, workflowID)
	for jobID, ref := range m.jobToNode {
		if ref.workflowID == workflowID {
			delete(m.jobToNode, jobID)
		}
	}
}

func readyNodesLocked(runtime *workflowRuntime) []string {
	ready := make([]string, 0)
	for _, nodeID := range runtime.topo {
		if runtime.enqueued[nodeID] || runtime.completed[nodeID] {
			continue
		}
		node := runtime.nodesByID[nodeID]
		allDepsDone := true
		for _, dep := range node.DependsOn {
			if !runtime.completed[dep] {
				allDepsDone = false
				break
			}
		}
		if allDepsDone {
			ready = append(ready, nodeID)
		}
	}
	return ready
}

func jobFromNode(runtime *workflowRuntime, node WorkflowNode) Job {
	args := append([]any(nil), node.Args...)
	args = appendDependencyScalarArgs(args, runtime.completedOutputs, node.DependsOn)

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

func normalizeAndValidateWorkflow(spec WorkflowSpec) (WorkflowSpec, map[string]WorkflowNode, []string, error) {
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

	topo, err := topologicalSort(nodesByID)
	if err != nil {
		return WorkflowSpec{}, nil, nil, err
	}

	return WorkflowSpec{
		ID:    spec.ID,
		Nodes: normalizedNodes,
	}, nodesByID, topo, nil
}

func topologicalSort(nodesByID map[string]WorkflowNode) ([]string, error) {
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
	sort.Strings(ready)

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
		sort.Strings(ready)
	}

	if len(out) != len(nodesByID) {
		return nil, errors.New("workflow graph contains cycle")
	}
	return out, nil
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
