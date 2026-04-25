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
	replicationFactor := loadReplicationFactor()
	maxResultPayloadBytes := loadMaxResultPayloadBytes()
	var workerAuth *workerAuth
	if !loadWorkerAuthDisabled() {
		auth, err := newWorkerAuth(loadWorkerJWTSecret(), loadWorkerJWTTTL(), loadWorkerAuthChallengeTTL())
		if err != nil {
			log.Fatalf("failed to initialize worker auth: %v", err)
		}
		workerAuth = auth
	}
	engine := scheduler.NewEngine(scheduler.Config{
		ReplicationFactor: replicationFactor,
		AssignmentTTL:     30 * time.Second,
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
	pendingPayments, err := store.ListPendingPaymentEvents(context.Background())
	if err != nil {
		log.Fatalf("failed to load pending payments: %v", err)
	}
	engine.RestorePendingPayments(pendingPayments)
	stopPaymentsProcessor := startPaymentsProcessor(engine, store, loadPaymentsProcessInterval())
	defer stopPaymentsProcessor()
	stopAssignmentJanitor := startAssignmentJanitor(engine, 5*time.Second)
	defer stopAssignmentJanitor()

	if err := bootstrapWorkflow(engine, workflowManager, store); err != nil {
		log.Fatal(err)
	}

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
