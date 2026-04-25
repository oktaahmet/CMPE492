package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// This file contains result validation and acceptance policy logic. It keeps
// worker-provided result_sig as a claim, but consensus is based on the
// canonical server digest of result_payload.

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
	if _, alreadySubmitted := state.submitted[req.WorkerID]; alreadySubmitted {
		return e.buildDecision(state), nil
	}

	// A worker can submit only while its assignment reservation is alive. If the
	// browser stopped and the TTL expired, a late result is rejected instead of
	// racing a replacement worker's attempt.
	if _, assigned := state.assignments[req.WorkerID]; !assigned {
		if state.finalized {
			return e.buildDecision(state), nil
		}
		return Decision{}, errors.New("worker was not assigned for this job")
	}
	if err := validateResultPayload(req.ResultPayload, state.job.ResultSchema); err != nil {
		return Decision{}, err
	}
	submissionDigest, err := canonicalSubmissionDigest(req.ResultPayload)
	if err != nil {
		return Decision{}, err
	}

	delete(state.assignments, req.WorkerID)
	state.submitted[req.WorkerID] = submissionDigest
	state.submittedPayload[req.WorkerID] = cloneJSONMap(req.ResultPayload)

	requiredReplicas := replicationFactorForJob(state.job, e.cfg.ReplicationFactor)
	policy := NormalizeAcceptancePolicy(string(state.job.AcceptancePolicy))
	acceptedSig, acceptedWorkers := majority(state.submitted)
	if policy == AcceptancePolicyCollectAll {
		// Stochastic jobs use collect_all: every required replica is paid and the
		// downstream workflow receives a deterministic wrapper containing all samples.
		if len(state.submitted) >= requiredReplicas {
			if !state.finalized {
				state.finalized = true
				e.finalizedJobs++
			}
			acceptedPayload := collectAllAcceptedPayload(state.submittedPayload)
			acceptedDigest, err := canonicalSubmissionDigest(acceptedPayload)
			if err != nil {
				return Decision{}, err
			}
			state.acceptedPayload = acceptedPayload
			state.acceptedResult = acceptedDigest
			state.acceptedWorkers = len(state.submitted)
			e.appendMissingPaymentEventsLocked(state.job, acceptedDigest, submittedWorkerIDs(state.submitted))
		}
	} else if acceptedSig != "" && acceptedWorkers >= quorum(requiredReplicas) {
		if !state.finalized {
			state.finalized = true
			e.finalizedJobs++
		}
		acceptedWorkerIDs := workersForSig(state.submitted, acceptedSig)
		state.acceptedPayload = pickAcceptedPayload(state.submittedPayload, acceptedWorkerIDs)
		state.acceptedResult = acceptedSig
		state.acceptedWorkers = acceptedWorkers
		e.appendMissingPaymentEventsLocked(state.job, acceptedSig, acceptedWorkerIDs)
	}

	if !state.finalized && policy == AcceptancePolicyConsensus && len(state.submitted) >= requiredReplicas {
		// A full replica round without quorum means the answers split too widely.
		// Clear only the round data so the same job can be reassigned and retried.
		resetSubmissionRoundLocked(state)
	}

	if jobClosedForAssignment(state) {
		e.maybeCompactQueueLocked()
	}

	return e.buildDecision(state), nil
}

func submittedWorkerIDs(submitted map[string]string) []string {
	workers := make([]string, 0, len(submitted))
	for workerID := range submitted {
		workers = append(workers, workerID)
	}
	sort.Strings(workers)
	return workers
}

func collectAllAcceptedPayload(payloadByWorker map[string]map[string]any) map[string]any {
	workerIDs := make([]string, 0, len(payloadByWorker))
	for workerID := range payloadByWorker {
		workerIDs = append(workerIDs, workerID)
	}
	sort.Strings(workerIDs)

	samples := make([]any, 0, len(workerIDs))
	for _, workerID := range workerIDs {
		samples = append(samples, cloneJSONMap(payloadByWorker[workerID]))
	}
	return map[string]any{
		"output": map[string]any{
			"sample_count": len(samples),
			"samples":      samples,
		},
	}
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
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return cloneJSONMap(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return val
	}
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

func canonicalSubmissionDigest(payload map[string]any) (string, error) {
	if payload == nil {
		payload = map[string]any{}
	}

	// json.Marshal sorts map keys, giving a stable digest for equivalent JSON
	// objects regardless of field order in the browser submission.
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("result payload canonicalization failed: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
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
