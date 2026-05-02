package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

func activateWorkflow(
	ctx context.Context,
	workflowID string,
	resetState bool,
	topologyMode scheduler.TopologyMode,
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) (adminWorkflowActivateResponse, error) {
	// Resolve and prepare the spec before touching the live workflow so a bad
	// upload cannot partially replace the current in-memory runtime.
	path, err := resolveWorkflowSpecPathByID(workflowID)
	if err != nil {
		if os.IsNotExist(err) {
			return adminWorkflowActivateResponse{}, fmt.Errorf("workflow not found: %s", workflowID)
		}
		return adminWorkflowActivateResponse{}, err
	}

	spec, completedOutputs, err := prepareWorkflowLoad(ctx, path, store)
	if err != nil {
		return adminWorkflowActivateResponse{}, err
	}
	if resetState {
		completedOutputs = nil
	}

	// Probe-load into an isolated manager first. This validates the same
	// normalized spec and recovered state that will be loaded for real below.
	probeManager := scheduler.NewWorkflowManager()
	probeManager.SetTopologyMode(topologyMode)
	if _, _, err := probeManager.LoadWorkflowWithCompleted(spec, completedOutputs); err != nil {
		return adminWorkflowActivateResponse{}, err
	}

	current := workflowManager.ActiveWorkflowID()
	previousPersistedID, err := store.GetActiveWorkflowID(ctx)
	if err != nil {
		return adminWorkflowActivateResponse{}, fmt.Errorf("failed to read active workflow id: %w", err)
	}
	previousPersistedMode, err := store.GetTopologyMode(ctx)
	if err != nil {
		return adminWorkflowActivateResponse{}, fmt.Errorf("failed to read topology mode: %w", err)
	}

	if err := persistActiveWorkflowMetadata(ctx, store, spec.ID, topologyMode); err != nil {
		return adminWorkflowActivateResponse{}, err
	}

	if resetState {
		if err := store.DeleteWorkflowState(ctx, spec.ID); err != nil {
			restoreActiveWorkflowMetadata(ctx, store, previousPersistedID, previousPersistedMode)
			return adminWorkflowActivateResponse{}, fmt.Errorf("failed to reset workflow state: %w", err)
		}
	}

	if current != "" {
		workflowManager.ClearWorkflow()
		engine.RemoveWorkflowJobs(current)
	}

	previousMode := workflowManager.TopologyMode()
	workflowManager.SetTopologyMode(topologyMode)
	result, jobs, err := workflowManager.LoadWorkflowWithCompleted(spec, completedOutputs)
	if err != nil {
		workflowManager.SetTopologyMode(previousMode)
		restoreActiveWorkflowMetadata(ctx, store, previousPersistedID, previousPersistedMode)
		return adminWorkflowActivateResponse{}, err
	}
	if err := dispatchWorkflowJobs(ctx, engine, workflowManager, store, jobs); err != nil {
		workflowManager.ClearWorkflow()
		workflowManager.SetTopologyMode(previousMode)
		restoreActiveWorkflowMetadata(ctx, store, previousPersistedID, previousPersistedMode)
		return adminWorkflowActivateResponse{}, err
	}

	return adminWorkflowActivateResponse{
		WorkflowID:      spec.ID,
		ResetState:      resetState,
		TopologyMode:    string(topologyMode),
		RecoveredNodes:  len(completedOutputs),
		EnqueuedJobs:    len(result.EnqueuedJobIDs),
		TopologicalSize: len(result.TopologicalOrder),
	}, nil
}

func persistActiveWorkflowMetadata(ctx context.Context, store *postgres.Store, workflowID string, topologyMode scheduler.TopologyMode) error {
	if err := store.SetActiveWorkflowID(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to persist active workflow id: %w", err)
	}
	if err := store.SetTopologyMode(ctx, string(topologyMode)); err != nil {
		return fmt.Errorf("failed to persist topology mode: %w", err)
	}
	return nil
}

func restoreActiveWorkflowMetadata(ctx context.Context, store *postgres.Store, workflowID string, topologyMode string) {
	if workflowID == "" {
		if err := store.ClearActiveWorkflowID(ctx); err != nil {
			log.Printf("failed to restore empty active workflow id: %v", err)
		}
	} else if err := store.SetActiveWorkflowID(ctx, workflowID); err != nil {
		log.Printf("failed to restore active workflow id: %v", err)
	}
	if topologyMode != "" {
		if err := store.SetTopologyMode(ctx, topologyMode); err != nil {
			log.Printf("failed to restore topology mode: %v", err)
		}
	}
}

// deleteWorkflow removes only uploaded workflows. Built-in workflow examples are
// intentionally protected by checking that the resolved spec lives in its upload
// directory before deleting files.
func deleteWorkflow(
	ctx context.Context,
	workflowID string,
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) error {
	specPath, err := resolveWorkflowSpecPathByID(workflowID)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("workflow not found: %s", workflowID)
		}
		return err
	}

	uploadedDir := uploadedWorkflowDir(workflowID)
	isUploaded, err := pathInsideDir(specPath, uploadedDir)
	if err != nil {
		return err
	}
	if !isUploaded {
		return fmt.Errorf("only uploaded workflows can be deleted: %s", workflowID)
	}

	if workflowManager.ActiveWorkflowID() == workflowID {
		workflowManager.ClearWorkflow()
		engine.RemoveWorkflowJobs(workflowID)
	}

	if err := store.DeleteWorkflowState(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to delete workflow state: %w", err)
	}
	activeID, err := store.GetActiveWorkflowID(ctx)
	if err != nil {
		return err
	}
	if activeID == workflowID {
		if err := store.ClearActiveWorkflowID(ctx); err != nil {
			return err
		}
	}

	if err := os.RemoveAll(uploadedDir); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join("static", "uploaded", workflowID))
	invalidateWorkflowSpecIndexCache()
	invalidateWorkflowArtifactSpecCache()
	return nil
}

// loadWorkflowFromPath is the boot-time loader. It prepares the spec, replays
// persisted node outputs, loads the in-memory DAG, and dispatches any nodes that
// become runnable after recovery.
func loadWorkflowFromPath(
	ctx context.Context,
	path string,
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) (scheduler.WorkflowSpec, scheduler.WorkflowLoadResult, int, error) {
	normalized, completedOutputs, err := prepareWorkflowLoad(ctx, path, store)
	if err != nil {
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}
	if activeID := workflowManager.ActiveWorkflowID(); activeID != "" {
		return normalized, scheduler.WorkflowLoadResult{}, 0, fmt.Errorf("workflow already loaded: %s", activeID)
	}

	result, jobs, err := workflowManager.LoadWorkflowWithCompleted(normalized, completedOutputs)
	if err != nil {
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}
	if err := dispatchWorkflowJobs(ctx, engine, workflowManager, store, jobs); err != nil {
		workflowManager.ClearWorkflow()
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}

	return normalized, result, len(completedOutputs), nil
}

// prepareWorkflowLoad performs all file/database work needed before a workflow
// can be loaded: read JSON, resolve program paths, validate, hydrate artifacts,
// build missing wasm outputs, and recover completed node outputs.
func prepareWorkflowLoad(
	ctx context.Context,
	path string,
	store *postgres.Store,
) (scheduler.WorkflowSpec, map[string]map[string]any, error) {
	spec, err := readWorkflowSpecFromPath(path)
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}
	spec, err = resolveWorkflowPrograms(spec, path)
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}
	normalized, err := scheduler.ValidateWorkflowSpec(spec)
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}
	if insideUploadedDir, insideErr := pathInsideDir(path, uploadedWorkflowsDir); insideErr != nil {
		return scheduler.WorkflowSpec{}, nil, insideErr
	} else if insideUploadedDir {
		if err := validateUploadedWorkflowWasmURLs(normalized); err != nil {
			return scheduler.WorkflowSpec{}, nil, err
		}
	}
	normalized, err = hydrateWorkflowArtifacts(normalized, path)
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}
	if err := ensureWorkflowArtifacts(normalized, path); err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}

	completedOutputs, err := store.LoadWorkflowCompletedOutputs(ctx, normalized.ID)
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}
	return normalized, completedOutputs, nil
}
