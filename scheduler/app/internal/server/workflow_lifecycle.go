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
	ctx := context.Background()

	// Boot from the persisted active workflow when possible; otherwise fall back
	// to the bundled example so a fresh database still starts with runnable work.
	bootPath := strings.TrimSpace(os.Getenv("WORKFLOW_BOOT_FILE"))
	if bootPath == "" {
		bootPath = filepath.Join("workflows", "prime-example", "prime-example.json")
	}
	topologyMode, err := store.GetTopologyMode(ctx)
	if err != nil {
		log.Printf("failed to read topology mode from db: %v", err)
		topologyMode = ""
	}
	workflowManager.SetTopologyMode(scheduler.NormalizeTopologyMode(topologyMode))
	activeID, err := store.GetActiveWorkflowID(ctx)
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

	spec, result, recovered, err := loadWorkflowFromPath(ctx, bootPath, engine, workflowManager, store)
	if err != nil {
		return fmt.Errorf("workflow bootstrap load failed (%s): %w", bootPath, err)
	}
	// Persist the resolved boot target. This repairs stale/missing metadata after
	// falling back to the default workflow.
	if err := store.SetActiveWorkflowID(ctx, spec.ID); err != nil {
		log.Printf("failed to persist active workflow id during bootstrap: %v", err)
	}
	if err := store.SetTopologyMode(ctx, string(workflowManager.TopologyMode())); err != nil {
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
	// Look up the authoritative workflow/node identity from the engine instead
	// of trusting request body fields beyond the job_id.
	workflowID, nodeID, found := engine.JobIdentity(jobID)
	if !found {
		return fmt.Errorf("job identity not found for finalized result")
	}
	// Store the accepted output before unlocking children; downstream jobs load
	// dependencies exclusively from durable workflow state.
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

	// WorkflowManager only receives the already-finalized payload, so browser
	// consensus stays isolated inside Engine.
	nextJobs, err := workflowManager.OnJobFinalized(jobID, output)
	if err != nil {
		return fmt.Errorf("failed to progress workflow: %w", err)
	}
	if err := dispatchWorkflowJobs(ctx, engine, workflowManager, store, nextJobs); err != nil {
		return fmt.Errorf("failed to enqueue unlocked workflow jobs: %w", err)
	}

	triggerPaymentProcessingAsync(ctx, engine, store)
	return nil
}
