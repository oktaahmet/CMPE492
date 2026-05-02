package server

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"x402-scheduler/internal/scheduler"
)

const (
	defaultNodeOutputChunkLimit = 256
	maxNodeOutputChunkLimit     = 16384
)

type artifactFileSignature struct {
	path    string
	modTime time.Time
	size    int64
}

type workflowArtifactSpecCacheEntry struct {
	specModTime time.Time
	specSize    int64
	artifacts   map[string]artifactFileSignature
	spec        scheduler.WorkflowSpec
}

var workflowArtifactSpecCache = struct {
	sync.Mutex
	entries map[string]workflowArtifactSpecCacheEntry
}{
	entries: map[string]workflowArtifactSpecCacheEntry{},
}

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
		normalized, err := loadWorkflowArtifactSpec(specPath)
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
	// The output body is not needed here; this existing store call is just the
	// completion gate that prevents serving declared output files before the node
	// has finalized.
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
		if !isSafeWorkflowID(workflowID) || !isSafeWorkflowID(nodeID) {
			http.Error(w, "invalid workflow_id or node_id", http.StatusBadRequest)
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
		if !isSafeWorkflowID(workflowID) || !isSafeWorkflowID(nodeID) {
			http.Error(w, "invalid workflow_id or node_id", http.StatusBadRequest)
			return
		}

		offset := parsePositiveIntQuery(r.URL.Query().Get("offset"), 0)
		limit := parseChunkLimitQuery(r.URL.Query().Get("limit"))

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

func parseChunkLimitQuery(raw string) int {
	limit := parsePositiveIntQuery(raw, defaultNodeOutputChunkLimit)
	if limit <= 0 {
		return defaultNodeOutputChunkLimit
	}
	if limit > maxNodeOutputChunkLimit {
		return maxNodeOutputChunkLimit
	}
	return limit
}

// loadWorkflowArtifactSpec parses, validates, and hydrates the workflow metadata
// needed by artifact serving. The cache avoids repeated JSON parsing and input
// artifact hashing on hot artifact URLs.
func loadWorkflowArtifactSpec(specPath string) (scheduler.WorkflowSpec, error) {
	info, err := os.Stat(specPath)
	if err != nil {
		return scheduler.WorkflowSpec{}, err
	}
	if info.IsDir() {
		return scheduler.WorkflowSpec{}, os.ErrInvalid
	}
	if spec, ok := cachedWorkflowArtifactSpec(specPath, info); ok {
		return spec, nil
	}

	spec, err := readWorkflowSpecFromPath(specPath)
	if err != nil {
		return scheduler.WorkflowSpec{}, err
	}
	spec, err = resolveWorkflowPrograms(spec, specPath)
	if err != nil {
		return scheduler.WorkflowSpec{}, err
	}
	normalized, err := scheduler.ValidateWorkflowSpec(spec)
	if err != nil {
		return scheduler.WorkflowSpec{}, err
	}
	normalized, err = hydrateWorkflowArtifacts(normalized, specPath)
	if err != nil {
		return scheduler.WorkflowSpec{}, err
	}
	cacheWorkflowArtifactSpec(specPath, info, normalized)
	return normalized, nil
}

// cachedWorkflowArtifactSpec reuses an entry only while both the workflow spec
// and its static input artifact files still have the same size and modtime.
func cachedWorkflowArtifactSpec(specPath string, info os.FileInfo) (scheduler.WorkflowSpec, bool) {
	workflowArtifactSpecCache.Lock()
	entry, ok := workflowArtifactSpecCache.entries[specPath]
	workflowArtifactSpecCache.Unlock()
	if !ok || !entry.specModTime.Equal(info.ModTime()) || entry.specSize != info.Size() {
		return scheduler.WorkflowSpec{}, false
	}
	if !artifactFileSignaturesMatch(entry.artifacts) {
		return scheduler.WorkflowSpec{}, false
	}
	return entry.spec, true
}

// cacheWorkflowArtifactSpec skips caching if any input artifact disappeared
// between hydration and cache insertion.
func cacheWorkflowArtifactSpec(specPath string, info os.FileInfo, spec scheduler.WorkflowSpec) {
	signatures, ok := collectArtifactFileSignatures(spec.Artifacts)
	if !ok {
		return
	}

	workflowArtifactSpecCache.Lock()
	workflowArtifactSpecCache.entries[specPath] = workflowArtifactSpecCacheEntry{
		specModTime: info.ModTime(),
		specSize:    info.Size(),
		artifacts:   signatures,
		spec:        spec,
	}
	workflowArtifactSpecCache.Unlock()
}

func invalidateWorkflowArtifactSpecCache() {
	workflowArtifactSpecCache.Lock()
	workflowArtifactSpecCache.entries = map[string]workflowArtifactSpecCacheEntry{}
	workflowArtifactSpecCache.Unlock()
}

func collectArtifactFileSignatures(artifacts []scheduler.WorkflowArtifact) (map[string]artifactFileSignature, bool) {
	signatures := make(map[string]artifactFileSignature, len(artifacts))
	for _, artifact := range artifacts {
		info, err := os.Stat(artifact.LocalPath)
		if err != nil || info.IsDir() {
			return nil, false
		}
		signatures[artifact.ID] = artifactFileSignature{
			path:    artifact.LocalPath,
			modTime: info.ModTime(),
			size:    info.Size(),
		}
	}
	return signatures, true
}

func artifactFileSignaturesMatch(signatures map[string]artifactFileSignature) bool {
	for _, signature := range signatures {
		info, err := os.Stat(signature.path)
		if err != nil || info.IsDir() {
			return false
		}
		if !signature.modTime.Equal(info.ModTime()) || signature.size != info.Size() {
			return false
		}
	}
	return true
}
