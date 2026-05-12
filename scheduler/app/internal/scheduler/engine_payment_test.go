package scheduler

import (
	"errors"
	"testing"
	"time"
)

type stubPaymentProvider struct {
	receipts map[string]PaymentReceipt
	errors   map[string]error
}

func (s stubPaymentProvider) Transfer(event PaymentEvent) (PaymentReceipt, error) {
	if err := s.errors[event.ID]; err != nil {
		return PaymentReceipt{}, err
	}
	if receipt, ok := s.receipts[event.ID]; ok {
		return receipt, nil
	}
	return PaymentReceipt{}, nil
}

func TestEngineProcessPaymentsTransitionsStatuses(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: time.Minute,
	})
	engine.SetPaymentProvider(stubPaymentProvider{
		receipts: map[string]PaymentReceipt{
			"evt-ok": {
				TxHash:  "0xtx",
				Network: "base-sepolia",
				Payer:   "0xpayer",
			},
		},
		errors: map[string]error{
			"evt-retry": errors.New("temporary failure"),
		},
	})
	engine.RestorePendingPayments([]PaymentEvent{
		{
			ID:         "evt-ok",
			JobID:      "job-1",
			WorkflowID: "wf",
			WorkerID:   "w1",
			Status:     "pending_x402_transfer",
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		{
			ID:         "evt-retry",
			JobID:      "job-2",
			WorkflowID: "wf",
			WorkerID:   "w2",
			Status:     "retry",
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
	})

	processed, err := engine.ProcessPayments(nil)
	if err != nil {
		t.Fatalf("ProcessPayments() error = %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 confirmed payment, got %d", processed)
	}

	events := engine.PaymentQueueSnapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 payment events, got %d", len(events))
	}

	byID := map[string]PaymentEvent{}
	for _, event := range events {
		byID[event.ID] = event
	}
	if got := byID["evt-ok"]; got.Status != "confirmed" || got.TxHash != "0xtx" || got.Attempts != 1 {
		t.Fatalf("unexpected confirmed payment event: %#v", got)
	}
	if got := byID["evt-retry"]; got.Status != "retry" || got.LastError == "" || got.Attempts != 1 {
		t.Fatalf("unexpected retry payment event: %#v", got)
	}
}

func TestEngineProcessPaymentsRevertsProcessingStateWhenPersistFails(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: time.Minute,
	})
	engine.SetPaymentProvider(stubPaymentProvider{
		receipts: map[string]PaymentReceipt{
			"evt-ok": {TxHash: "0xtx"},
		},
	})
	engine.RestorePendingPayments([]PaymentEvent{
		{
			ID:         "evt-ok",
			JobID:      "job-1",
			WorkflowID: "wf",
			WorkerID:   "w1",
			Status:     PaymentStatusPending,
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
	})

	persistCalls := 0
	processed, err := engine.ProcessPayments(func(_ []PaymentEvent) error {
		persistCalls++
		return errors.New("db down")
	})
	if err == nil {
		t.Fatal("expected persist error")
	}
	if processed != 0 {
		t.Fatalf("expected no processed payments, got %d", processed)
	}
	if persistCalls != 1 {
		t.Fatalf("expected one persist attempt, got %d", persistCalls)
	}

	events := engine.PaymentQueueSnapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 payment event, got %d", len(events))
	}
	if events[0].Status != PaymentStatusPending {
		t.Fatalf("expected payment to revert to pending, got %#v", events[0])
	}
}

func TestEngineRestorePendingPaymentsMarksInterruptedProcessingAndAllowsRequeue(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{
		AssignmentTTL: time.Minute,
	})
	engine.RestorePendingPayments([]PaymentEvent{
		{
			ID:         "evt-processing",
			JobID:      "job-1",
			WorkflowID: "wf",
			WorkerID:   "w1",
			Status:     PaymentStatusProcessing,
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		{
			ID:         "evt-retry",
			JobID:      "job-2",
			WorkflowID: "wf",
			WorkerID:   "w2",
			Status:     PaymentStatusRetry,
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		{
			ID:         "evt-review",
			JobID:      "job-3",
			WorkflowID: "wf",
			WorkerID:   "w3",
			Status:     PaymentStatusReview,
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
	})

	events := engine.PaymentQueueSnapshot()
	if len(events) != 3 {
		t.Fatalf("expected 3 restored events, got %d", len(events))
	}

	byID := map[string]PaymentEvent{}
	for _, event := range events {
		byID[event.ID] = event
	}
	if got := byID["evt-processing"]; got.Status != PaymentStatusReview || got.LastError == "" {
		t.Fatalf("expected interrupted processing event to require review, got %#v", got)
	}
	if got := byID["evt-review"]; got.Status != PaymentStatusReview {
		t.Fatalf("expected existing review event to stay requeueable, got %#v", got)
	}

	requeued := engine.RequeuePaymentsByStatus(PaymentStatusReview, PaymentStatusRetry, "manual retry")
	if requeued != 2 {
		t.Fatalf("expected 2 events to be requeued, got %d", requeued)
	}

	events = engine.PaymentQueueSnapshot()
	byID = map[string]PaymentEvent{}
	for _, event := range events {
		byID[event.ID] = event
	}
	if got := byID["evt-processing"]; got.Status != PaymentStatusRetry || got.LastError != "manual retry" {
		t.Fatalf("expected reviewed event to move back to retry, got %#v", got)
	}
	if got := byID["evt-review"]; got.Status != PaymentStatusRetry || got.LastError != "manual retry" {
		t.Fatalf("expected existing review event to move back to retry, got %#v", got)
	}
}
