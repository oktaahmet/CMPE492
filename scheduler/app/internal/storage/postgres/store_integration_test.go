package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"x402-scheduler/internal/scheduler"
)

func TestStoreIntegrationWorkflowStateAndPayments(t *testing.T) {
	ctx := context.Background()
	store := newDisposableStore(t, ctx)

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	output := map[string]any{
		"output": "hello",
		"count":  float64(2),
	}
	if err := store.UpsertWorkflowNodeCompletion(ctx, "wf-int", "node-a", "job-a", "hash-a", output, time.Now().UTC()); err != nil {
		t.Fatalf("UpsertWorkflowNodeCompletion() error = %v", err)
	}

	loaded, found, err := store.LoadWorkflowNodeOutput(ctx, "wf-int", "node-a")
	if err != nil {
		t.Fatalf("LoadWorkflowNodeOutput() error = %v", err)
	}
	if !found || loaded["output"] != "hello" || loaded["count"] != float64(2) {
		t.Fatalf("unexpected loaded output: found=%v output=%#v", found, loaded)
	}

	completed, err := store.LoadWorkflowCompletedOutputs(ctx, "wf-int")
	if err != nil {
		t.Fatalf("LoadWorkflowCompletedOutputs() error = %v", err)
	}
	if completed["node-a"]["output"] != "hello" {
		t.Fatalf("unexpected completed outputs: %#v", completed)
	}

	if err := store.SetActiveWorkflowID(ctx, "wf-int"); err != nil {
		t.Fatalf("SetActiveWorkflowID() error = %v", err)
	}
	if err := store.SetTopologyMode(ctx, "priority_aware"); err != nil {
		t.Fatalf("SetTopologyMode() error = %v", err)
	}
	if got, err := store.GetActiveWorkflowID(ctx); err != nil || got != "wf-int" {
		t.Fatalf("GetActiveWorkflowID() = %q, %v", got, err)
	}
	if got, err := store.GetTopologyMode(ctx); err != nil || got != "priority_aware" {
		t.Fatalf("GetTopologyMode() = %q, %v", got, err)
	}
	if err := store.ClearActiveWorkflowID(ctx); err != nil {
		t.Fatalf("ClearActiveWorkflowID() error = %v", err)
	}
	if got, err := store.GetActiveWorkflowID(ctx); err != nil || got != "" {
		t.Fatalf("GetActiveWorkflowID() after clear = %q, %v", got, err)
	}

	older := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	newer := time.Now().UTC().Format(time.RFC3339)
	events := []scheduler.PaymentEvent{
		{
			ID:           "evt-pending",
			JobID:        "job-a",
			WorkflowID:   "wf-int",
			WorkerID:     "0x0000000000000000000000000000000000000001",
			AmountUSDC:   "0.01",
			AcceptedHash: "hash-a",
			Status:       scheduler.PaymentStatusPending,
			UpdatedAt:    older,
		},
		{
			ID:           "evt-confirmed",
			JobID:        "job-b",
			WorkflowID:   "wf-int",
			WorkerID:     "0x0000000000000000000000000000000000000001",
			AmountUSDC:   "0.02",
			AcceptedHash: "hash-b",
			Status:       scheduler.PaymentStatusConfirmed,
			UpdatedAt:    newer,
			TxHash:       "0xtx",
		},
	}
	if err := store.UpsertPaymentEvents(ctx, events); err != nil {
		t.Fatalf("UpsertPaymentEvents() insert error = %v", err)
	}

	events[0].Status = scheduler.PaymentStatusRetry
	events[0].Attempts = 1
	events[0].LastError = "temporary failure"
	events[0].UpdatedAt = newer
	if err := store.UpsertPaymentEvents(ctx, events[:1]); err != nil {
		t.Fatalf("UpsertPaymentEvents() update error = %v", err)
	}

	workerEvents, err := store.ListPaymentEventsForWorker(ctx, "0x0000000000000000000000000000000000000001")
	if err != nil {
		t.Fatalf("ListPaymentEventsForWorker() error = %v", err)
	}
	if len(workerEvents) != 2 {
		t.Fatalf("expected 2 worker events, got %#v", workerEvents)
	}
	workerEventsByID := map[string]scheduler.PaymentEvent{}
	for _, event := range workerEvents {
		workerEventsByID[event.ID] = event
	}
	if got := workerEventsByID["evt-pending"]; got.Status != scheduler.PaymentStatusRetry || got.Attempts != 1 || got.LastError == "" {
		t.Fatalf("unexpected upserted payment event: %#v", got)
	}
	if got := workerEventsByID["evt-confirmed"]; got.Status != scheduler.PaymentStatusConfirmed || got.TxHash != "0xtx" {
		t.Fatalf("unexpected confirmed payment event: %#v", got)
	}

	pendingEvents, err := store.ListPendingPaymentEvents(ctx)
	if err != nil {
		t.Fatalf("ListPendingPaymentEvents() error = %v", err)
	}
	if len(pendingEvents) != 1 || pendingEvents[0].ID != "evt-pending" {
		t.Fatalf("expected only retry event to be pending, got %#v", pendingEvents)
	}

	if err := store.DeleteWorkflowState(ctx, "wf-int"); err != nil {
		t.Fatalf("DeleteWorkflowState() error = %v", err)
	}
	if _, found, err := store.LoadWorkflowNodeOutput(ctx, "wf-int", "node-a"); err != nil || found {
		t.Fatalf("expected deleted workflow output to be missing: found=%v err=%v", found, err)
	}
}

func newDisposableStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	adminDSN := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if adminDSN == "" {
		t.Skip("set TEST_DATABASE_URL to run Postgres integration tests")
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}

	dbName := fmt.Sprintf("x402_scheduler_test_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+dbName); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create disposable db: %v", err)
	}

	testDSN := databaseDSN(adminDSN, dbName)
	store, err := NewStore(testDSN)
	if err != nil {
		_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS `+dbName)
		_ = adminDB.Close()
		t.Fatalf("NewStore() error = %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
		_, _ = adminDB.ExecContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, dbName)
		_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS `+dbName)
		_ = adminDB.Close()
	})

	return store
}

func databaseDSN(base string, dbName string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	parsed.Path = "/" + dbName
	return parsed.String()
}
