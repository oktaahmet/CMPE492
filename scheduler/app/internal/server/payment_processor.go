package server

import (
	"context"
	"log"
	"net/http"
	"time"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

func adminRequeueInterruptedPaymentsHandler(engine *scheduler.Engine, store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		requeued := engine.RequeuePaymentsByStatus(
			"needs_reconciliation",
			"retry",
			"manually requeued after interrupted processing review",
		)
		if err := store.UpsertPaymentEvents(r.Context(), engine.PaymentQueueSnapshot()); err != nil {
			http.Error(w, "failed to persist requeued payments", http.StatusInternalServerError)
			return
		}

		if requeued > 0 {
			triggerPaymentProcessingAsync(engine, store)
		}

		writeJSON(w, RequeuePaymentsResponse{RequeuedCount: requeued}, http.StatusOK)
	}
}

func triggerPaymentProcessingAsync(engine *scheduler.Engine, store *postgres.Store) {
	go func() {
		totalProcessed := 0
		for {
			processedCount, err := runPaymentSweep(context.Background(), engine, store)
			if err != nil {
				log.Printf("async payment sweep failed: %v", err)
				return
			}
			totalProcessed += processedCount

			if pendingPaymentCount(engine.PaymentQueueSnapshot()) == 0 {
				if totalProcessed > 0 {
					log.Printf("async payment sweep processed=%d", totalProcessed)
				}
				return
			}

			time.Sleep(2 * time.Second)
		}
	}()
}

func startPaymentsProcessor(engine *scheduler.Engine, store *postgres.Store, interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				processedCount, err := runPaymentSweep(ctx, engine, store)
				if err != nil {
					log.Printf("background payment sweep failed: %v", err)
					continue
				}
				if processedCount > 0 {
					log.Printf("background payment sweep processed=%d", processedCount)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return cancel
}

func startAssignmentJanitor(engine *scheduler.Engine, interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				engine.CleanupExpiredAssignments()
			case <-ctx.Done():
				return
			}
		}
	}()

	return cancel
}

func runPaymentSweep(ctx context.Context, engine *scheduler.Engine, store *postgres.Store) (int, error) {
	processedCount, err := engine.ProcessPayments(func(events []scheduler.PaymentEvent) error {
		return store.UpsertPaymentEvents(ctx, events)
	})
	if err != nil {
		return processedCount, err
	}
	if err := store.UpsertPaymentEvents(ctx, engine.PaymentQueueSnapshot()); err != nil {
		return processedCount, err
	}
	return processedCount, nil
}

func pendingPaymentCount(events []scheduler.PaymentEvent) int {
	count := 0
	for _, event := range events {
		if event.Status == "pending_x402_transfer" || event.Status == "retry" || event.Status == "processing_x402_transfer" {
			count++
		}
	}
	return count
}
