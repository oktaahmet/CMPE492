package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

const (
	workflowsRootDir     = "workflows"
	uploadedWorkflowsDir = "workflows/uploaded"
	uploadedWorkflowFile = "workflow.json"
	uploadedCPPDirName   = "cpp"
	maxUploadFilenameLen = 128
	maxWorkflowInputSize = 10 << 20
)

// adminWorkflowListHandler godoc
// @Summary      List workflows and active workflow id
// @Tags         admin
// @Produce      json
// @Param        Authorization  header    string                    true  "Bearer admin token"
// @Success      200            {object}  adminWorkflowListResponse
// @Failure      401            {string}  string
// @Failure      405            {string}  string
// @Router       /api/admin/workflows [get]
func adminWorkflowListHandler(
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		availableIDs, err := discoverWorkflowIDs()
		if err != nil {
			http.Error(w, "failed to list workflows", http.StatusInternalServerError)
			return
		}
		loaded := workflowManager.ActiveWorkflowID()
		activeID, err := store.GetActiveWorkflowID(r.Context())
		if err != nil {
			http.Error(w, "failed to read active workflow", http.StatusInternalServerError)
			return
		}
		topologyMode, err := store.GetTopologyMode(r.Context())
		if err != nil {
			http.Error(w, "failed to read topology mode", http.StatusInternalServerError)
			return
		}
		if topologyMode == "" {
			topologyMode = string(workflowManager.TopologyMode())
		}
		writeJSON(w, adminWorkflowListResponse{
			ActiveWorkflowID: activeID,
			LoadedWorkflowID: loaded,
			TopologyMode:     string(scheduler.NormalizeTopologyMode(topologyMode)),
			UploadedIDs:      availableIDs,
		}, http.StatusOK)
	}
}

// adminWorkflowUploadHandler godoc
// @Summary      Upload workflow JSON and C++ sources
// @Tags         admin
// @Accept       mpfd
// @Produce      json
// @Param        Authorization  header    string  true   "Bearer admin token"
// @Param        workflow_json  formData  file    true   "Workflow JSON file"
// @Param        cpp_files      formData  file    true   "One or more C++ files (.cpp)"
// @Param        workflow_input_files formData file false "Optional workflow input artifact files, written under data/ (max 10MB each)"
// @Success      201            {object}  map[string]any
// @Failure      400            {string}  string
// @Failure      401            {string}  string
// @Failure      405            {string}  string
// @Failure      409            {string}  string
// @Failure      500            {string}  string
// @Router       /api/admin/workflows/upload [post]
func adminWorkflowUploadHandler(
	workflowManager *scheduler.WorkflowManager,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
			return
		}

		workflowHeader, err := requiredFilePart(r, "workflow_json")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		spec, prettyJSON, err := parseAndNormalizeWorkflowJSON(workflowHeader)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !isSafeWorkflowID(spec.ID) {
			http.Error(w, "workflow id must use letters, numbers, underscore or dash", http.StatusBadRequest)
			return
		}
		if err := validateUploadedWorkflowWasmURLs(spec); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cppHeaders := r.MultipartForm.File["cpp_files"]
		if len(cppHeaders) == 0 {
			http.Error(w, "at least one cpp_files part is required", http.StatusBadRequest)
			return
		}
		inputHeaders := r.MultipartForm.File["workflow_input_files"]

		if workflowManager.ActiveWorkflowID() == spec.ID {
			http.Error(w, "workflow already loaded; activate another workflow first", http.StatusConflict)
			return
		}
		if _, err := resolveWorkflowSpecPathByID(spec.ID); err == nil {
			http.Error(w, "workflow id already exists", http.StatusConflict)
			return
		}

		if err := writeUploadedWorkflow(spec.ID, prettyJSON, cppHeaders, inputHeaders); err != nil {
			if cleanupErr := cleanupUploadedWorkflowArtifacts(spec.ID); cleanupErr != nil {
				log.Printf("failed to cleanup uploaded workflow after write error: workflow_id=%s err=%v", spec.ID, cleanupErr)
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := ensureWorkflowArtifacts(spec, uploadedWorkflowSpecPath(spec.ID)); err != nil {
			if cleanupErr := cleanupUploadedWorkflowArtifacts(spec.ID); cleanupErr != nil {
				log.Printf("failed to cleanup uploaded workflow after compile error: workflow_id=%s err=%v", spec.ID, cleanupErr)
			}
			// Uploaded source compile errors are authoring problems, but missing
			// compilers or filesystem failures are server setup/runtime problems.
			http.Error(w, fmt.Sprintf("workflow uploaded but artifact build failed: %v", err), workflowBuildErrorStatus(err))
			return
		}

		writeJSON(w, map[string]any{
			"workflow_id": spec.ID,
			"uploaded":    true,
			"path":        uploadedWorkflowSpecPath(spec.ID),
		}, http.StatusCreated)
	}
}

func workflowBuildErrorStatus(err error) int {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return http.StatusInternalServerError
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

// adminWorkflowActivateHandler godoc
// @Summary      Activate workflow by id
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization  header    string                        true  "Bearer admin token"
// @Param        body           body      adminWorkflowActivateRequest  true  "Activate request"
// @Success      200            {object}  adminWorkflowActivateResponse
// @Failure      400            {string}  string
// @Failure      401            {string}  string
// @Failure      405            {string}  string
// @Router       /api/admin/workflows/activate [post]
func adminWorkflowActivateHandler(
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req adminWorkflowActivateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		req.WorkflowID = strings.TrimSpace(req.WorkflowID)
		if !isSafeWorkflowID(req.WorkflowID) {
			http.Error(w, "invalid workflow_id", http.StatusBadRequest)
			return
		}
		if req.TopologyMode != "" && !scheduler.IsValidTopologyMode(req.TopologyMode) {
			http.Error(w, "invalid topology_mode", http.StatusBadRequest)
			return
		}

		topologyMode := workflowManager.TopologyMode()
		if req.TopologyMode != "" {
			topologyMode = scheduler.NormalizeTopologyMode(req.TopologyMode)
		}

		resp, err := activateWorkflow(r.Context(), req.WorkflowID, req.ResetState, topologyMode, engine, workflowManager, store)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, resp, http.StatusOK)
	}
}

// adminWorkflowDeleteHandler godoc
// @Summary      Delete uploaded workflow and its persisted state
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization  header    string                      true  "Bearer admin token"
// @Param        body           body      adminWorkflowDeleteRequest  true  "Delete request"
// @Success      200            {object}  adminWorkflowDeleteResponse
// @Failure      400            {string}  string
// @Failure      401            {string}  string
// @Failure      405            {string}  string
// @Router       /api/admin/workflows/delete [post]
func adminWorkflowDeleteHandler(
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req adminWorkflowDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		req.WorkflowID = strings.TrimSpace(req.WorkflowID)
		if !isSafeWorkflowID(req.WorkflowID) {
			http.Error(w, "invalid workflow_id", http.StatusBadRequest)
			return
		}

		if err := deleteWorkflow(r.Context(), req.WorkflowID, engine, workflowManager, store); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeJSON(w, adminWorkflowDeleteResponse{
			WorkflowID: req.WorkflowID,
			Deleted:    true,
		}, http.StatusOK)
	}
}

func runtimeHandler(
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		activeID, err := store.GetActiveWorkflowID(r.Context())
		if err != nil {
			http.Error(w, "failed to read active workflow", http.StatusInternalServerError)
			return
		}
		loadedID := workflowManager.ActiveWorkflowID()

		resp := adminRuntimeResponse{
			ActiveWorkflowID: activeID,
			LoadedWorkflowID: loadedID,
			TopologyMode:     string(workflowManager.TopologyMode()),
			Stats:            engine.StatsSnapshot(),
		}

		// The live graph exists only for the in-memory loaded workflow. activeID
		// may be persisted even when this process has not loaded that workflow.
		if loadedID != "" {
			if snapshot, ok := workflowManager.Snapshot(); ok {
				resp.Workflow = &snapshot
				resp.Jobs = engine.WorkflowJobSnapshots(loadedID)
			}
		}

		writeJSON(w, resp, http.StatusOK)
	}
}
