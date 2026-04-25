package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

func bootstrapWorkflow(
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) error {
	bootPath := strings.TrimSpace(os.Getenv("WORKFLOW_BOOT_FILE"))
	if bootPath == "" {
		bootPath = filepath.Join("workflows", "prime-example", "prime-example.json")
	}
	topologyMode, err := store.GetTopologyMode(context.Background())
	if err != nil {
		log.Printf("failed to read topology mode from db: %v", err)
		topologyMode = ""
	}
	workflowManager.SetTopologyMode(scheduler.NormalizeTopologyMode(topologyMode))
	activeID, err := store.GetActiveWorkflowID(context.Background())
	if err != nil {
		log.Printf("failed to read active workflow id from db: %v", err)
		activeID = ""
	}
	if activeID != "" {
		activePath, err := resolveWorkflowSpecPathByID(activeID)
		if err == nil {
			bootPath = activePath
		} else {
			log.Printf("active workflow file missing, fallback to default: workflow_id=%s", activeID)
		}
	}

	spec, result, recovered, err := loadWorkflowFromPath(context.Background(), bootPath, engine, workflowManager, store)
	if err != nil {
		return fmt.Errorf("workflow bootstrap load failed (%s): %w", bootPath, err)
	}
	if err := store.SetActiveWorkflowID(context.Background(), spec.ID); err != nil {
		log.Printf("failed to persist active workflow id during bootstrap: %v", err)
	}
	if err := store.SetTopologyMode(context.Background(), string(workflowManager.TopologyMode())); err != nil {
		log.Printf("failed to persist topology mode during bootstrap: %v", err)
	}
	log.Printf(
		"workflow bootstrapped: file=%s workflow_id=%s topology_mode=%s topo_nodes=%d recovered_completed=%d initial_jobs=%d",
		bootPath,
		spec.ID,
		workflowManager.TopologyMode(),
		len(result.TopologicalOrder),
		recovered,
		len(result.EnqueuedJobIDs),
	)
	return nil
}

func handleBrowserFinalizedDecision(
	ctx context.Context,
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
	jobID string,
	decision scheduler.Decision,
) error {
	output, ok := engine.FinalizedOutput(jobID)
	if !ok {
		output = map[string]any{}
	}
	workflowID, nodeID, found := engine.JobIdentity(jobID)
	if !found {
		return fmt.Errorf("job identity not found for finalized result")
	}
	if err := store.UpsertWorkflowNodeCompletion(
		ctx,
		workflowID,
		nodeID,
		jobID,
		decision.AcceptedResult,
		output,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("failed to persist finalized workflow node state: %w", err)
	}
	if err := store.UpsertPaymentEvents(ctx, engine.PaymentQueueSnapshot()); err != nil {
		return fmt.Errorf("failed to persist payment queue: %w", err)
	}

	nextJobs, err := workflowManager.OnJobFinalized(jobID, output)
	if err != nil {
		return fmt.Errorf("failed to progress workflow: %w", err)
	}
	if err := dispatchWorkflowJobs(ctx, engine, workflowManager, store, nextJobs); err != nil {
		return fmt.Errorf("failed to enqueue unlocked workflow jobs: %w", err)
	}

	triggerPaymentProcessingAsync(engine, store)
	return nil
}
