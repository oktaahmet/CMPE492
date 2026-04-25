package server

import (
	"context"

	"x402-scheduler/internal/scheduler"
)

type workflowOutputStore interface {
	LoadWorkflowNodeOutput(ctx context.Context, workflowID string, nodeID string) (map[string]any, bool, error)
}

type paymentEventStore interface {
	ListPaymentEventsForWorker(ctx context.Context, workerID string) ([]scheduler.PaymentEvent, error)
}
