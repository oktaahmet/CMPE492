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

	probeManager := scheduler.NewWorkflowManager()
	probeManager.SetTopologyMode(topologyMode)
	if _, _, err := probeManager.LoadWorkflowWithCompleted(spec, completedOutputs); err != nil {
		return adminWorkflowActivateResponse{}, err
	}

	current := workflowManager.ActiveWorkflowID()
	if resetState {
		if err := store.DeleteWorkflowState(ctx, spec.ID); err != nil {
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
		return adminWorkflowActivateResponse{}, err
	}
	if err := dispatchWorkflowJobs(ctx, engine, workflowManager, store, jobs); err != nil {
		workflowManager.ClearWorkflow()
		workflowManager.SetTopologyMode(previousMode)
		return adminWorkflowActivateResponse{}, err
	}
	if err := store.SetActiveWorkflowID(ctx, spec.ID); err != nil {
		log.Printf("failed to persist active workflow id: %v", err)
	}
	if err := store.SetTopologyMode(ctx, string(topologyMode)); err != nil {
		log.Printf("failed to persist topology mode: %v", err)
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
	return nil
}

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
