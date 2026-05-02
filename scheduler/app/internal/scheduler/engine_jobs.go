package scheduler

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file contains job lifecycle operations: enqueueing, assigning work to
// browser/server workers, heartbeat/TTL cleanup, and runtime snapshots.

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
	if job.ReplicationFactor < 0 {
		return errors.New("replication_factor must be >= 0")
	}
	if !IsValidAcceptancePolicy(string(job.AcceptancePolicy)) {
		return errors.New("acceptance_policy is invalid")
	}
	job.AcceptancePolicy = NormalizeAcceptancePolicy(string(job.AcceptancePolicy))

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

	// Assignment is a reservation, not a dequeue. If a browser stops before
	// submitting, the reservation expires by TTL and another worker can retry.
	for i := range e.queue {
		id := e.queue[i]
		state := e.jobs[id]
		if state == nil || jobClosedForAssignment(state) {
			continue
		}
		if _, already := state.assignments[workerID]; already {
			continue
		}
		if _, alreadySubmitted := state.submitted[workerID]; alreadySubmitted {
			continue
		}
		requiredReplicas := replicationFactorForJob(state.job)
		if len(state.assignments)+len(state.submitted) >= requiredReplicas {
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
			Artifacts:    cloneArtifacts(state.job.Artifacts),
			RewardUSDC:   state.job.RewardUSDC,
			RequiredRep:  requiredReplicas,
		}, true
	}

	return Assignment{}, false
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

func (e *Engine) CleanupExpiredAssignments() {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	e.cleanupExpiredAssignments(now)
	e.cleanupExpiredWorkersLocked(now)
}

func (e *Engine) StatsSnapshot() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()

	pendingPayments := 0
	for _, p := range e.paymentEvents {
		if p.Status == paymentStatusPending || p.Status == paymentStatusRetry || p.Status == paymentStatusProcessing {
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

func (e *Engine) WorkflowJobSnapshots(workflowID string) []JobRuntimeSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	if strings.TrimSpace(workflowID) == "" {
		return nil
	}

	queueIndexByJobID := make(map[string]int, len(e.queue))
	for idx, jobID := range e.queue {
		queueIndexByJobID[jobID] = idx
	}

	out := make([]JobRuntimeSnapshot, 0)
	for jobID, state := range e.jobs {
		if state == nil || state.job.WorkflowID != workflowID {
			continue
		}
		assignedWorkers := make([]string, 0, len(state.assignments))
		for workerID := range state.assignments {
			assignedWorkers = append(assignedWorkers, workerID)
		}
		sort.Strings(assignedWorkers)

		submittedWorkers := make([]string, 0, len(state.submitted))
		for workerID := range state.submitted {
			submittedWorkers = append(submittedWorkers, workerID)
		}
		sort.Strings(submittedWorkers)

		acceptedWorkers := acceptedWorkersForState(state)
		requiredReplicas := replicationFactorForJob(state.job)
		out = append(out, JobRuntimeSnapshot{
			JobID:            jobID,
			WorkflowID:       state.job.WorkflowID,
			NodeID:           state.job.NodeID,
			AssignedWorkers:  assignedWorkers,
			SubmittedWorkers: submittedWorkers,
			Finalized:        state.finalized,
			AcceptedWorkers:  acceptedWorkers,
			QueueIndex:       queueIndexByJobID[jobID],
			RequiredReplicas: requiredReplicas,
			AcceptancePolicy: state.job.AcceptancePolicy,
			Traits:           append([]string(nil), state.job.Traits...),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].QueueIndex == out[j].QueueIndex {
			return out[i].NodeID < out[j].NodeID
		}
		return out[i].QueueIndex < out[j].QueueIndex
	})
	return out
}

func (e *Engine) buildDecision(state *jobState) Decision {
	sig, count := majority(state.submitted)
	if state.finalized {
		sig = state.acceptedResult
		count = state.acceptedWorkers
	}
	return Decision{
		JobID:            state.job.ID,
		Finalized:        state.finalized,
		AcceptedResult:   sig,
		AcceptedPayload:  cloneJSONMap(state.acceptedPayload),
		AcceptedWorkers:  count,
		RequiredReplicas: replicationFactorForJob(state.job),
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

func (e *Engine) cleanupExpiredWorkersLocked(now time.Time) {
	for workerID, worker := range e.workers {
		if now.Sub(worker.LastHeartbeat) > e.cfg.AssignmentTTL {
			delete(e.workers, workerID)
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
		if st := e.jobs[id]; st != nil && !jobClosedForAssignment(st) {
			total++
		}
	}
	return total
}

func (e *Engine) maybeCompactQueueLocked() {
	if len(e.queue) < 1024 {
		return
	}

	// queue is intentionally lazy. Compact only after it grows enough that the
	// stale finalized/deleted IDs become worth rebuilding away.
	active := 0
	for _, id := range e.queue {
		if st := e.jobs[id]; st != nil && !jobClosedForAssignment(st) {
			active++
		}
	}
	if active*2 > len(e.queue) {
		return
	}

	out := make([]string, 0, active)
	for _, id := range e.queue {
		if st := e.jobs[id]; st != nil && !jobClosedForAssignment(st) {
			out = append(out, id)
		}
	}
	e.queue = out
}

func jobClosedForAssignment(state *jobState) bool {
	if state == nil {
		return true
	}
	return state.finalized
}

func resetSubmissionRoundLocked(state *jobState) {
	// Split consensus rounds discard only unfinished round state. The job itself
	// remains queued so another replica round can attempt consensus again.
	state.assignments = map[string]time.Time{}
	state.submitted = map[string]string{}
	state.submittedPayload = map[string]map[string]any{}
}

func replicationFactorForJob(job Job) int {
	if job.ReplicationFactor > 0 {
		return job.ReplicationFactor
	}
	return 1
}

func acceptedWorkersForState(state *jobState) int {
	if state == nil {
		return 0
	}
	if state.finalized {
		return state.acceptedWorkers
	}
	if NormalizeAcceptancePolicy(string(state.job.AcceptancePolicy)) == AcceptancePolicyCollectAll {
		return len(state.submitted)
	}
	_, acceptedWorkers := majority(state.submitted)
	return acceptedWorkers
}
