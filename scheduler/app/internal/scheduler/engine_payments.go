package scheduler

import (
	"fmt"
	"strings"
	"time"
)

// This file contains the in-memory payment queue. The server persists snapshots
// around these operations so x402 transfers can be recovered after restarts.

func (e *Engine) SetPaymentProvider(provider PaymentProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if provider != nil {
		e.paymentClient = provider
	}
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
		switch event.Status {
		case paymentStatusPending, paymentStatusRetry:
		case paymentStatusProcessing:
			// A previous process may have crashed after marking work as processing.
			// Do not blindly retry it; surface it for explicit reconciliation first.
			event.Status = paymentStatusReview
			if strings.TrimSpace(event.LastError) == "" {
				event.LastError = "restored after interrupted processing; review before retry"
			}
		default:
			continue
		}
		if seen[event.ID] {
			continue
		}
		e.paymentEventsByID[event.ID] = len(e.paymentEvents)
		e.paymentEvents = append(e.paymentEvents, event)
		seen[event.ID] = true
	}
}

func (e *Engine) RequeuePaymentsByStatus(fromStatus string, toStatus string, reason string) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	fromStatus = strings.TrimSpace(fromStatus)
	toStatus = strings.TrimSpace(toStatus)
	if fromStatus == "" || toStatus == "" {
		return 0
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339)
	count := 0
	for i := range e.paymentEvents {
		event := &e.paymentEvents[i]
		if event.Status != fromStatus {
			continue
		}
		event.Status = toStatus
		event.UpdatedAt = updatedAt
		if trimmed := strings.TrimSpace(reason); trimmed != "" {
			event.LastError = trimmed
		}
		count++
	}
	return count
}

func (e *Engine) ProcessPayments(persistIntermediate func([]PaymentEvent) error) (int, error) {
	e.mu.Lock()
	if e.processingPayments {
		e.mu.Unlock()
		return 0, nil
	}
	if e.paymentClient == nil {
		e.mu.Unlock()
		return 0, nil
	}
	e.processingPayments = true

	type workItem struct {
		index int
		id    string
		event PaymentEvent
		prev  PaymentEvent
	}
	items := make([]workItem, 0)
	startedAt := time.Now().UTC().Format(time.RFC3339)
	for i := range e.paymentEvents {
		event := &e.paymentEvents[i]
		if event.Status != paymentStatusPending && event.Status != paymentStatusRetry {
			continue
		}
		items = append(items, workItem{
			index: i,
			id:    event.ID,
			event: *event,
			prev:  *event,
		})
		event.Status = paymentStatusProcessing
		event.UpdatedAt = startedAt
		event.LastError = ""
	}
	client := e.paymentClient
	snapshot := make([]PaymentEvent, len(e.paymentEvents))
	copy(snapshot, e.paymentEvents)
	e.mu.Unlock()

	if len(items) == 0 {
		e.mu.Lock()
		e.processingPayments = false
		e.mu.Unlock()
		return 0, nil
	}

	if persistIntermediate != nil {
		if err := persistIntermediate(snapshot); err != nil {
			e.mu.Lock()
			defer e.mu.Unlock()
			// The "processing" marker must reach durable storage before any transfer
			// is attempted; otherwise a crash could hide work that already left memory.
			for _, item := range items {
				if item.index < 0 || item.index >= len(e.paymentEvents) {
					continue
				}
				event := &e.paymentEvents[item.index]
				if event.ID != item.id || event.Status != paymentStatusProcessing {
					continue
				}
				*event = item.prev
			}
			e.processingPayments = false
			return 0, err
		}
	}

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
		if event.Status != paymentStatusProcessing {
			continue
		}
		event.Attempts++
		event.UpdatedAt = finishedAt
		if result.err != nil {
			event.Status = paymentStatusRetry
			event.LastError = result.err.Error()
			continue
		}

		event.Status = paymentStatusConfirmed
		event.LastError = ""
		event.TxHash = result.receipt.TxHash
		event.Network = result.receipt.Network
		event.Payer = result.receipt.Payer
		processed++
	}

	return processed, nil
}

func (e *Engine) appendMissingPaymentEventsLocked(job Job, acceptedSig string, workerIDs []string) {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, workerID := range workerIDs {
		id := paymentEventID(job.ID, workerID)
		if e.hasPaymentEventLocked(id) {
			continue
		}
		e.paymentEventsByID[id] = len(e.paymentEvents)
		e.paymentEvents = append(e.paymentEvents, PaymentEvent{
			ID:           id,
			JobID:        job.ID,
			WorkflowID:   job.WorkflowID,
			AmountUSDC:   job.RewardUSDC,
			WorkerID:     workerID,
			AcceptedHash: acceptedSig,
			Status:       paymentStatusPending,
			UpdatedAt:    now,
		})
	}
}

func (e *Engine) hasPaymentEventLocked(id string) bool {
	idx, exists := e.paymentEventsByID[id]
	return exists && idx >= 0 && idx < len(e.paymentEvents)
}

func paymentEventID(jobID, workerID string) string {
	return fmt.Sprintf("%s:%s", jobID, workerID)
}
