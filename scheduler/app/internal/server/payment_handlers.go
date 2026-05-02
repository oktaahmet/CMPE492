package server

import (
	"net/http"
	"strings"
)

// paymentsHandler godoc
// @Summary      List payment queue
// @Tags         payments
// @Produce      json
// @Param        worker_id  query     string  false  "Worker ID; inferred from bearer token when worker auth is enabled"
// @Success      200  {array}   scheduler.PaymentEvent
// @Failure      400  {string}  string
// @Failure      405  {string}  string
// @Router       /api/payments [get]
func paymentsHandler(store paymentEventStore, auth *workerAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		workerID := strings.TrimSpace(r.URL.Query().Get("worker_id"))
		// When worker auth is enabled, the token owns the payment history scope.
		// A query worker_id may narrow to the same wallet, never another worker.
		if auth != nil {
			authWorkerID, err := auth.authenticateRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			if workerID == "" {
				workerID = authWorkerID
			} else {
				normalizedWorkerID, normalizeErr := normalizeWorkerID(workerID)
				if normalizeErr != nil {
					http.Error(w, normalizeErr.Error(), http.StatusBadRequest)
					return
				}
				if !sameWorkerID(authWorkerID, normalizedWorkerID) {
					http.Error(w, "worker token does not match worker_id", http.StatusForbidden)
					return
				}
				workerID = normalizedWorkerID
			}
		} else if workerID != "" {
			normalizedWorkerID, err := normalizeWorkerID(workerID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			workerID = normalizedWorkerID
		}
		if workerID == "" {
			http.Error(w, "worker_id is required", http.StatusBadRequest)
			return
		}
		events, err := store.ListPaymentEventsForWorker(r.Context(), workerID)
		if err != nil {
			http.Error(w, "failed to load payments", http.StatusInternalServerError)
			return
		}
		writeJSON(w, events, http.StatusOK)
	}
}
