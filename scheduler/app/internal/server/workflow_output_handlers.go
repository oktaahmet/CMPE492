package server

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"x402-scheduler/internal/scheduler"
)

// workflowArtifactHandler serves static input artifacts and finalized server output artifacts.
func workflowArtifactHandler(store workflowOutputStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id"))
		nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
		artifactID := strings.TrimSpace(r.URL.Query().Get("artifact_id"))
		if workflowID == "" || artifactID == "" {
			http.Error(w, "workflow_id and artifact_id are required", http.StatusBadRequest)
			return
		}
		if !isSafeWorkflowID(workflowID) || (nodeID != "" && !isSafeWorkflowID(nodeID)) {
			http.Error(w, "invalid workflow_id or node_id", http.StatusBadRequest)
			return
		}

		specPath, err := resolveWorkflowSpecPathByID(workflowID)
		if err != nil {
			http.Error(w, "workflow not found", http.StatusNotFound)
			return
		}
		spec, err := readWorkflowSpecFromPath(specPath)
		if err != nil {
			http.Error(w, "failed to read workflow", http.StatusInternalServerError)
			return
		}
		spec, err = resolveWorkflowPrograms(spec, specPath)
		if err != nil {
			http.Error(w, "workflow program resolution failed", http.StatusInternalServerError)
			return
		}
		normalized, err := scheduler.ValidateWorkflowSpec(spec)
		if err != nil {
			http.Error(w, "workflow validation failed", http.StatusInternalServerError)
			return
		}
		normalized, err = hydrateWorkflowArtifacts(normalized, specPath)
		if err != nil {
			http.Error(w, "artifact metadata failed", http.StatusInternalServerError)
			return
		}

		if nodeID != "" {
			serveOutputArtifact(w, r, store, normalized, workflowID, nodeID, artifactID)
			return
		}

		for _, artifact := range normalized.Artifacts {
			if artifact.ID != artifactID {
				continue
			}
			w.Header().Set("Content-Type", artifact.ContentType)
			w.Header().Set("X-Artifact-ID", artifact.ID)
			w.Header().Set("X-Artifact-SHA256", artifact.SHA256)
			w.Header().Set("X-Artifact-Size", strconv.FormatInt(artifact.Size, 10))
			http.ServeFile(w, r, artifact.LocalPath)
			return
		}

		http.Error(w, "artifact not found", http.StatusNotFound)
	}
}

func serveOutputArtifact(
	w http.ResponseWriter,
	r *http.Request,
	store workflowOutputStore,
	normalized scheduler.WorkflowSpec,
	workflowID string,
	nodeID string,
	artifactID string,
) {
	if store == nil {
		http.Error(w, "artifact store unavailable", http.StatusInternalServerError)
		return
	}
	if _, found, err := store.LoadWorkflowNodeOutput(r.Context(), workflowID, nodeID); err != nil {
		http.Error(w, "failed to load workflow node output", http.StatusInternalServerError)
		return
	} else if !found {
		http.Error(w, "workflow node output not found", http.StatusNotFound)
		return
	}
	for _, node := range normalized.Nodes {
		if node.ID != nodeID {
			continue
		}
		for _, artifact := range node.OutputArtifacts {
			if artifact.ID != artifactID {
				continue
			}
			localPath, err := outputArtifactLocalPath(workflowID, nodeID, artifact.Path)
			if err != nil {
				http.Error(w, "artifact path invalid", http.StatusInternalServerError)
				return
			}
			info, err := os.Stat(localPath)
			if err != nil || info.IsDir() {
				http.Error(w, "artifact not found", http.StatusNotFound)
				return
			}
			contentType := strings.TrimSpace(artifact.ContentType)
			if contentType == "" {
				contentType = mime.TypeByExtension(filepath.Ext(localPath))
			}
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			sum, err := sha256File(localPath)
			if err != nil {
				http.Error(w, "artifact hash failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("X-Artifact-ID", artifact.ID)
			w.Header().Set("X-Artifact-SHA256", sum)
			w.Header().Set("X-Artifact-Size", strconv.FormatInt(info.Size(), 10))
			http.ServeFile(w, r, localPath)
			return
		}
	}
	http.Error(w, "artifact not found", http.StatusNotFound)
}

// workflowNodeOutputHandler godoc
// @Summary      Get finalized workflow node output
// @Tags         workflow
// @Produce      json
// @Param        workflow_id  query     string  true  "Workflow ID"
// @Param        node_id      query     string  true  "Node ID"
// @Success      200          {object}  map[string]any
// @Failure      400          {string}  string
// @Failure      404          {string}  string
// @Failure      405          {string}  string
// @Router       /api/workflow/node-output [get]
func workflowNodeOutputHandler(store workflowOutputStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id"))
		nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
		if workflowID == "" || nodeID == "" {
			http.Error(w, "workflow_id and node_id are required", http.StatusBadRequest)
			return
		}

		output, found, err := store.LoadWorkflowNodeOutput(r.Context(), workflowID, nodeID)
		if err != nil {
			http.Error(w, "failed to load workflow node output", http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "workflow node output not found", http.StatusNotFound)
			return
		}

		writeJSON(w, output, http.StatusOK)
	}
}

// workflowNodeOutputChunkHandler godoc
// @Summary      Get chunk of finalized workflow node output
// @Tags         workflow
// @Produce      json
// @Param        workflow_id  query     string  true   "Workflow ID"
// @Param        node_id      query     string  true   "Node ID"
// @Param        offset       query     int     false  "Chunk offset"
// @Param        limit        query     int     false  "Chunk size"
// @Success      200          {object}  NodeOutputChunkResponse
// @Failure      400          {string}  string
// @Failure      404          {string}  string
// @Failure      405          {string}  string
// @Router       /api/workflow/node-output/chunk [get]
func workflowNodeOutputChunkHandler(store workflowOutputStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id"))
		nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
		if workflowID == "" || nodeID == "" {
			http.Error(w, "workflow_id and node_id are required", http.StatusBadRequest)
			return
		}

		offset := parsePositiveIntQuery(r.URL.Query().Get("offset"), 0)
		limit := parsePositiveIntQuery(r.URL.Query().Get("limit"), 256)
		if limit > 16384 {
			limit = 16384
		}

		output, found, err := store.LoadWorkflowNodeOutput(r.Context(), workflowID, nodeID)
		if err != nil {
			http.Error(w, "failed to load workflow node output", http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "workflow node output not found", http.StatusNotFound)
			return
		}

		value, hasValue := output["output"]
		chunk, err := nodeOutputChunkFromValue(value, hasValue, offset, limit)
		if err != nil {
			http.Error(w, "failed to encode output", http.StatusInternalServerError)
			return
		}
		writeJSON(w, chunk, http.StatusOK)
	}
}

func parsePositiveIntQuery(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
