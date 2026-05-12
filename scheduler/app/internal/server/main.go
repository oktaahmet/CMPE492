package server

import (
	"context"
	"log"
	"net/http"
	"time"

	_ "x402-scheduler/docs"
	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

func Main() {
	// Load request-size and auth settings before constructing handlers so route
	// wiring stays immutable after the server starts.
	maxResultPayloadBytes := loadMaxResultPayloadBytes()
	var workerAuth *workerAuth
	if !loadWorkerAuthDisabled() {
		auth, err := newWorkerAuth(loadWorkerJWTSecret(), loadWorkerJWTTTL(), loadWorkerAuthChallengeTTL())
		if err != nil {
			log.Fatalf("failed to initialize worker auth: %v", err)
		}
		workerAuth = auth
	}

	// Engine owns volatile scheduling state; WorkflowManager owns DAG
	// progression. Durable workflow/payment state is restored from Postgres
	// below before routes are registered.
	engine := scheduler.NewEngine(scheduler.Config{
		AssignmentTTL: loadAssignmentTTL(),
	})
	workflowManager := scheduler.NewWorkflowManager()
	engine.SetPaymentProvider(loadPaymentProvider())

	store, err := postgres.NewStore(loadPostgresDSN())
	if err != nil {
		log.Fatalf("failed to connect postgres: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			log.Printf("failed to close postgres connection: %v", closeErr)
		}
	}()
	if err := store.Migrate(context.Background()); err != nil {
		log.Fatalf("failed to run postgres migrations: %v", err)
	}
	// Payments that were pending before a restart are replayed into memory. Any
	// transfer that crashed mid-processing is marked for manual reconciliation by
	// Engine.RestorePendingPayments.
	pendingPayments, err := store.ListPendingPaymentEvents(context.Background())
	if err != nil {
		log.Fatalf("failed to load pending payments: %v", err)
	}
	engine.RestorePendingPayments(pendingPayments)
	stopPaymentsProcessor := startPaymentsProcessor(engine, store, loadPaymentsProcessInterval())
	defer stopPaymentsProcessor()
	stopAssignmentJanitor := startAssignmentJanitor(engine, 5*time.Second)
	defer stopAssignmentJanitor()

	// Bootstrap after payment recovery so server-side workflow nodes can enqueue
	// payment events against a fully initialized engine/store pair.
	if err := bootstrapWorkflow(engine, workflowManager, store); err != nil {
		log.Fatal(err)
	}

	// All handlers share the same in-memory engine/manager and Postgres store;
	// auth is nil when worker auth has been explicitly disabled.
	registerRoutes(routeDeps{
		Engine:                engine,
		WorkflowManager:       workflowManager,
		Store:                 store,
		WorkerAuth:            workerAuth,
		MaxResultPayloadBytes: maxResultPayloadBytes,
		AdminToken:            loadAdminAPIToken(),
	})

	log.Println("Starting server on port 8080...")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
