package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"x402-scheduler/internal/scheduler"
)

type fakePaymentEventStore struct {
	events      []scheduler.PaymentEvent
	err         error
	gotWorkerID string
}

func (s *fakePaymentEventStore) ListPaymentEventsForWorker(_ context.Context, workerID string) ([]scheduler.PaymentEvent, error) {
	s.gotWorkerID = workerID
	return s.events, s.err
}

func TestPaymentsHandlerListsEventsAndNormalizesWorkerFilter(t *testing.T) {
	t.Parallel()

	store := &fakePaymentEventStore{
		events: []scheduler.PaymentEvent{
			{ID: "evt-1", WorkerID: "0x0000000000000000000000000000000000000001", Status: "confirmed"},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/payments?worker_id=0x0000000000000000000000000000000000000001", nil)
	rec := httptest.NewRecorder()

	paymentsHandler(store, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if store.gotWorkerID != "0x0000000000000000000000000000000000000001" {
		t.Fatalf("unexpected worker filter: %q", store.gotWorkerID)
	}
	var events []scheduler.PaymentEvent
	if err := json.NewDecoder(rec.Body).Decode(&events); err != nil {
		t.Fatalf("decode payments: %v", err)
	}
	if len(events) != 1 || events[0].ID != "evt-1" {
		t.Fatalf("unexpected payment events: %#v", events)
	}
}

func TestPaymentsHandlerRejectsInvalidWorkerFilterAndStoreError(t *testing.T) {
	t.Parallel()

	invalidReq := httptest.NewRequest(http.MethodGet, "/api/payments?worker_id=not-a-wallet", nil)
	invalidRec := httptest.NewRecorder()
	paymentsHandler(&fakePaymentEventStore{}, nil).ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body=%s", invalidRec.Code, invalidRec.Body.String())
	}

	missingWorkerReq := httptest.NewRequest(http.MethodGet, "/api/payments", nil)
	missingWorkerRec := httptest.NewRecorder()
	paymentsHandler(&fakePaymentEventStore{}, nil).ServeHTTP(missingWorkerRec, missingWorkerReq)
	if missingWorkerRec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing worker bad request, got %d body=%s", missingWorkerRec.Code, missingWorkerRec.Body.String())
	}

	errReq := httptest.NewRequest(http.MethodGet, "/api/payments?worker_id=0x0000000000000000000000000000000000000001", nil)
	errRec := httptest.NewRecorder()
	paymentsHandler(&fakePaymentEventStore{err: errors.New("db down")}, nil).ServeHTTP(errRec, errReq)
	if errRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected server error, got %d body=%s", errRec.Code, errRec.Body.String())
	}
}
