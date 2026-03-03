package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	ReplicationFactor int
	AssignmentTTL     time.Duration
}

type PaymentProvider interface {
	Transfer(event PaymentEvent) (PaymentReceipt, error)
}

type Engine struct {
	mu sync.Mutex

	cfg Config

	jobs               map[string]*jobState
	queue              []string
	paymentEvents      []PaymentEvent
	workers            map[string]Worker
	finalizedJobs      int
	paymentClient      PaymentProvider
	processingPayments bool
}

type jobState struct {
	job Job

	assignments      map[string]time.Time
	submitted        map[string]string
	submittedPayload map[string]map[string]any
	acceptedPayload  map[string]any
	finalized        bool
}

type Assignment struct {
	JobID        string          `json:"job_id"`
	WorkflowID   string          `json:"workflow_id"`
	NodeID       string          `json:"node_id"`
	WasmURL      string          `json:"wasm_url"`
	Args         []any           `json:"args,omitempty"`
	Dependencies []DependencyRef `json:"dependencies,omitempty"`
	RewardUSDC   string          `json:"reward_usdc"`
	RequiredRep  int             `json:"required_rep"`
}

type ResultSubmission struct {
	JobID         string         `json:"job_id"`
	WorkerID      string         `json:"worker_id"`
	ResultSig     string         `json:"result_sig"`
	ResultPayload map[string]any `json:"result_payload,omitempty"`
}

type Decision struct {
	JobID            string         `json:"job_id"`
	Finalized        bool           `json:"finalized"`
	AcceptedResult   string         `json:"accepted_result,omitempty"`
	AcceptedPayload  map[string]any `json:"accepted_payload,omitempty"`
	AcceptedWorkers  int            `json:"accepted_workers"`
	RequiredReplicas int            `json:"required_replicas"`
}

type PaymentEvent struct {
	ID           string `json:"id"`
	JobID        string `json:"job_id"`
	WorkflowID   string `json:"workflow_id"`
	AmountUSDC   string `json:"amount_usdc"`
	WorkerID     string `json:"worker_id"`
	AcceptedHash string `json:"accepted_hash"`
	Status       string `json:"status"`
	Attempts     int    `json:"attempts"`
	LastError    string `json:"last_error,omitempty"`
	UpdatedAt    string `json:"updated_at"`
	TxHash       string `json:"tx_hash,omitempty"`
	Network      string `json:"network,omitempty"`
	Payer        string `json:"payer,omitempty"`
}

func NewEngine(cfg Config) *Engine {
	if cfg.ReplicationFactor < 1 {
		cfg.ReplicationFactor = 1
	}
	if cfg.AssignmentTTL <= 0 {
		cfg.AssignmentTTL = 60 * time.Second
	}

	return &Engine{
		cfg:     cfg,
		jobs:    make(map[string]*jobState),
		workers: make(map[string]Worker),
	}
}

func (e *Engine) SetPaymentProvider(provider PaymentProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if provider != nil {
		e.paymentClient = provider
	}
}

func (e *Engine) Enqueue(job Job) error {
	if job.ID == "" {
		return errors.New("job id is required")
	}
	if job.WorkflowID == "" || job.NodeID == "" {
		return errors.New("workflow_id and node_id are required")
	}
	if job.WasmURL == "" {
		return errors.New("wasm_url is required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.jobs[job.ID]; exists {
		return fmt.Errorf("job already exists: %s", job.ID)
	}

	e.jobs[job.ID] = &jobState{
		job:              job,
		assignments:      map[string]time.Time{},
		submitted:        map[string]string{},
		submittedPayload: map[string]map[string]any{},
	}
	e.queue = append(e.queue, job.ID)

	return nil
}

func (e *Engine) AssignNext(workerID string) (Assignment, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.touchWorkerLocked(workerID)
	e.cleanupExpiredAssignments(time.Now())

	for i := range e.queue {
		id := e.queue[i]
		state := e.jobs[id]
		if state == nil || state.finalized {
			continue
		}
		if _, already := state.assignments[workerID]; already {
			continue
		}
		if _, alreadySubmitted := state.submitted[workerID]; alreadySubmitted {
			continue
		}
		if len(state.assignments)+len(state.submitted) >= e.cfg.ReplicationFactor {
			continue
		}

		state.assignments[workerID] = time.Now()
		return Assignment{
			JobID:        state.job.ID,
			WorkflowID:   state.job.WorkflowID,
			NodeID:       state.job.NodeID,
			WasmURL:      state.job.WasmURL,
			Args:         append([]any(nil), state.job.Args...),
			Dependencies: append([]DependencyRef(nil), state.job.Dependencies...),
			RewardUSDC:   state.job.RewardUSDC,
			RequiredRep:  e.cfg.ReplicationFactor,
		}, true
	}

	return Assignment{}, false
}

func (e *Engine) SubmitResult(req ResultSubmission) (Decision, error) {
	if req.JobID == "" || req.WorkerID == "" || req.ResultSig == "" {
		return Decision{}, errors.New("job_id, worker_id and result_sig are required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.jobs[req.JobID]
	if state == nil {
		return Decision{}, errors.New("job not found")
	}
	if state.finalized {
		return e.buildDecision(state), nil
	}

	if _, assigned := state.assignments[req.WorkerID]; !assigned {
		return Decision{}, errors.New("worker was not assigned for this job")
	}
	if err := validateResultPayload(req.ResultPayload, state.job.ResultSchema); err != nil {
		return Decision{}, err
	}

	delete(state.assignments, req.WorkerID)
	state.submitted[req.WorkerID] = req.ResultSig
	state.submittedPayload[req.WorkerID] = cloneJSONMap(req.ResultPayload)

	acceptedSig, acceptedWorkers := majority(state.submitted)
	if acceptedSig != "" && acceptedWorkers >= quorum(e.cfg.ReplicationFactor) {
		state.finalized = true
		e.finalizedJobs++
		acceptedWorkerIDs := workersForSig(state.submitted, acceptedSig)
		state.acceptedPayload = pickAcceptedPayload(state.submittedPayload, acceptedWorkerIDs)
		for _, workerID := range acceptedWorkerIDs {
			e.paymentEvents = append(e.paymentEvents, PaymentEvent{
				ID:           paymentEventID(state.job.ID, workerID),
				JobID:        state.job.ID,
				WorkflowID:   state.job.WorkflowID,
				AmountUSDC:   state.job.RewardUSDC,
				WorkerID:     workerID,
				AcceptedHash: acceptedSig,
				Status:       "pending_x402_transfer",
				UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			})
		}
		e.maybeCompactQueueLocked()
	}

	return e.buildDecision(state), nil
}

func (e *Engine) FinalizedOutput(jobID string) (map[string]any, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.jobs[jobID]
	if state == nil || !state.finalized {
		return nil, false
	}
	if state.acceptedPayload == nil {
		return map[string]any{}, true
	}
	return cloneJSONMap(state.acceptedPayload), true
}

func (e *Engine) PaymentQueueSnapshot() []PaymentEvent {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]PaymentEvent, len(e.paymentEvents))
	copy(out, e.paymentEvents)
	return out
}

func (e *Engine) RestorePendingPayments(events []PaymentEvent) {
	if len(events) == 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	seen := make(map[string]bool, len(e.paymentEvents))
	for _, event := range e.paymentEvents {
		seen[event.ID] = true
	}

	for _, event := range events {
		if event.Status != "pending_x402_transfer" && event.Status != "retry" {
			continue
		}
		if seen[event.ID] {
			continue
		}
		e.paymentEvents = append(e.paymentEvents, event)
		seen[event.ID] = true
	}
}

func (e *Engine) JobIdentity(jobID string) (workflowID string, nodeID string, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.jobs[jobID]
	if state == nil {
		return "", "", false
	}
	return state.job.WorkflowID, state.job.NodeID, true
}

func (e *Engine) RemoveWorkflowJobs(workflowID string) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	if strings.TrimSpace(workflowID) == "" {
		return 0
	}

	removed := 0
	for jobID, state := range e.jobs {
		if state == nil || state.job.WorkflowID != workflowID {
			continue
		}
		if state.finalized && e.finalizedJobs > 0 {
			e.finalizedJobs--
		}
		delete(e.jobs, jobID)
		removed++
	}
	if removed == 0 {
		return 0
	}

	out := make([]string, 0, len(e.queue))
	for _, id := range e.queue {
		if st := e.jobs[id]; st != nil {
			out = append(out, id)
		}
	}
	e.queue = out
	return removed
}

func (e *Engine) RegisterOrHeartbeat(workerID string) Worker {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.touchWorkerLocked(workerID)
}

func (e *Engine) StatsSnapshot() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()

	pendingPayments := 0
	for _, p := range e.paymentEvents {
		if p.Status == "pending_x402_transfer" || p.Status == "retry" {
			pendingPayments++
		}
	}

	return Stats{
		QueuedJobs:      e.queuedJobsLocked(),
		FinalizedJobs:   e.finalizedJobs,
		PendingPayments: pendingPayments,
		WorkersOnline:   e.onlineWorkersLocked(time.Now()),
		TotalJobs:       len(e.jobs),
	}
}

func (e *Engine) ProcessPayments() int {
	e.mu.Lock()
	if e.processingPayments {
		e.mu.Unlock()
		return 0
	}
	if e.paymentClient == nil {
		e.mu.Unlock()
		return 0
	}
	e.processingPayments = true

	type workItem struct {
		index int
		id    string
		event PaymentEvent
	}
	items := make([]workItem, 0)
	startedAt := time.Now().UTC().Format(time.RFC3339)
	for i := range e.paymentEvents {
		event := &e.paymentEvents[i]
		if event.Status != "pending_x402_transfer" && event.Status != "retry" {
			continue
		}
		event.Attempts++
		event.UpdatedAt = startedAt
		items = append(items, workItem{
			index: i,
			id:    event.ID,
			event: *event,
		})
	}
	client := e.paymentClient
	e.mu.Unlock()

	type paymentResult struct {
		index   int
		id      string
		receipt PaymentReceipt
		err     error
	}
	results := make([]paymentResult, 0, len(items))
	for _, item := range items {
		receipt, err := client.Transfer(item.event)
		results = append(results, paymentResult{
			index:   item.index,
			id:      item.id,
			receipt: receipt,
			err:     err,
		})
	}

	processed := 0
	finishedAt := time.Now().UTC().Format(time.RFC3339)

	e.mu.Lock()
	defer e.mu.Unlock()
	e.processingPayments = false

	for _, result := range results {
		if result.index < 0 || result.index >= len(e.paymentEvents) {
			continue
		}
		event := &e.paymentEvents[result.index]
		if event.ID != result.id {
			continue
		}
		if event.Status != "pending_x402_transfer" && event.Status != "retry" {
			continue
		}
		event.UpdatedAt = finishedAt
		if result.err != nil {
			event.Status = "retry"
			event.LastError = result.err.Error()
			continue
		}

		event.Status = "confirmed"
		event.LastError = ""
		event.TxHash = result.receipt.TxHash
		event.Network = result.receipt.Network
		event.Payer = result.receipt.Payer
		processed++
	}

	return processed
}

func (e *Engine) buildDecision(state *jobState) Decision {
	sig, count := majority(state.submitted)
	return Decision{
		JobID:            state.job.ID,
		Finalized:        state.finalized,
		AcceptedResult:   sig,
		AcceptedPayload:  cloneJSONMap(state.acceptedPayload),
		AcceptedWorkers:  count,
		RequiredReplicas: e.cfg.ReplicationFactor,
	}
}

func (e *Engine) cleanupExpiredAssignments(now time.Time) {
	for _, state := range e.jobs {
		for workerID, ts := range state.assignments {
			if now.Sub(ts) > e.cfg.AssignmentTTL {
				delete(state.assignments, workerID)
			}
		}
	}
}

func (e *Engine) touchWorkerLocked(workerID string) Worker {
	now := time.Now()
	w, ok := e.workers[workerID]
	if !ok {
		w = Worker{
			ID:          workerID,
			ConnectedAt: now,
		}
	}
	w.LastHeartbeat = now
	e.workers[workerID] = w
	return w
}

func (e *Engine) onlineWorkersLocked(now time.Time) int {
	count := 0
	for _, w := range e.workers {
		if now.Sub(w.LastHeartbeat) <= e.cfg.AssignmentTTL {
			count++
		}
	}
	return count
}

func (e *Engine) queuedJobsLocked() int {
	total := 0
	for _, id := range e.queue {
		if st := e.jobs[id]; st != nil && !st.finalized {
			total++
		}
	}
	return total
}

func (e *Engine) maybeCompactQueueLocked() {
	if len(e.queue) < 1024 {
		return
	}

	active := 0
	for _, id := range e.queue {
		if st := e.jobs[id]; st != nil && !st.finalized {
			active++
		}
	}
	if active*2 > len(e.queue) {
		return
	}

	out := make([]string, 0, active)
	for _, id := range e.queue {
		if st := e.jobs[id]; st != nil && !st.finalized {
			out = append(out, id)
		}
	}
	e.queue = out
}

func majority(m map[string]string) (string, int) {
	if len(m) == 0 {
		return "", 0
	}

	counts := map[string]int{}
	for _, sig := range m {
		counts[sig]++
	}

	type pair struct {
		sig   string
		count int
	}
	pairs := make([]pair, 0, len(counts))
	for sig, count := range counts {
		pairs = append(pairs, pair{sig: sig, count: count})
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].sig < pairs[j].sig
		}
		return pairs[i].count > pairs[j].count
	})

	return pairs[0].sig, pairs[0].count
}

func workersForSig(submitted map[string]string, sig string) []string {
	workers := make([]string, 0, len(submitted))
	for workerID, s := range submitted {
		if s == sig {
			workers = append(workers, workerID)
		}
	}
	sort.Strings(workers)
	return workers
}

func quorum(replicationFactor int) int {
	return (replicationFactor / 2) + 1
}

func paymentEventID(jobID, workerID string) string {
	return fmt.Sprintf("%s:%s", jobID, workerID)
}

func pickAcceptedPayload(payloadByWorker map[string]map[string]any, acceptedWorkerIDs []string) map[string]any {
	for _, workerID := range acceptedWorkerIDs {
		if payload := payloadByWorker[workerID]; payload != nil {
			return cloneJSONMap(payload)
		}
	}
	return map[string]any{}
}

func cloneJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		fallback := make(map[string]any, len(in))
		for k, v := range in {
			fallback[k] = v
		}
		return fallback
	}
	return out
}

func validateResultPayload(payload map[string]any, schema map[string]PayloadFieldRule) error {
	if len(schema) == 0 {
		return nil
	}
	if payload == nil {
		payload = map[string]any{}
	}

	for field, rule := range schema {
		expected := strings.ToLower(strings.TrimSpace(rule.Type))
		if expected == "" {
			return fmt.Errorf("result_schema.%s type is required", field)
		}

		value, exists := payload[field]
		if !exists {
			if rule.Required {
				return fmt.Errorf("result_payload.%s is required", field)
			}
			continue
		}

		if !matchesPayloadType(value, expected) {
			return fmt.Errorf("result_payload.%s must be %s", field, expected)
		}
	}
	return nil
}

func matchesPayloadType(value any, expected string) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		default:
			return false
		}
	case "boolean", "bool":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}
