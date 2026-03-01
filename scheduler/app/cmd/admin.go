package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

const (
	workflowsRootDir     = "workflows"
	uploadedWorkflowsDir = "workflows/uploaded"
	uploadedWorkflowFile = "workflow.json"
	uploadedCPPDirName   = "cpp"
)

type adminWorkflowListResponse struct {
	ActiveWorkflowID string   `json:"active_workflow_id,omitempty"`
	LoadedWorkflowID string   `json:"loaded_workflow_id,omitempty"`
	UploadedIDs      []string `json:"uploaded_ids"`
}

type adminWorkflowActivateRequest struct {
	WorkflowID string `json:"workflow_id"`
	ResetState bool   `json:"reset_state,omitempty"`
}

type adminWorkflowActivateResponse struct {
	WorkflowID      string `json:"workflow_id"`
	ResetState      bool   `json:"reset_state"`
	RecoveredNodes  int    `json:"recovered_nodes"`
	EnqueuedJobs    int    `json:"enqueued_jobs"`
	TopologicalSize int    `json:"topological_size"`
}

type adminWorkflowDeleteRequest struct {
	WorkflowID string `json:"workflow_id"`
}

type adminWorkflowDeleteResponse struct {
	WorkflowID string `json:"workflow_id"`
	Deleted    bool   `json:"deleted"`
}

func withAdminToken(adminToken string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminToken == "" {
			http.Error(w, "admin api disabled", http.StatusServiceUnavailable)
			return
		}
		incoming, ok := bearerTokenFromHeader(r.Header.Get("Authorization"))
		if !ok {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare([]byte(incoming), []byte(adminToken)) != 1 {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func bearerTokenFromHeader(value string) (string, bool) {
	value = strings.TrimSpace(value)
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}

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
		loadedIDs := workflowManager.WorkflowIDs()
		loaded := ""
		if len(loadedIDs) > 0 {
			loaded = loadedIDs[0]
		}
		activeID, err := store.GetActiveWorkflowID(r.Context())
		if err != nil {
			http.Error(w, "failed to read active workflow", http.StatusInternalServerError)
			return
		}
		writeJSON(w, adminWorkflowListResponse{
			ActiveWorkflowID: activeID,
			LoadedWorkflowID: loaded,
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
// @Success      201            {object}  map[string]any
// @Failure      400            {string}  string
// @Failure      401            {string}  string
// @Failure      405            {string}  string
// @Failure      409            {string}  string
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

		cppHeaders := r.MultipartForm.File["cpp_files"]
		if len(cppHeaders) == 0 {
			http.Error(w, "at least one cpp_files part is required", http.StatusBadRequest)
			return
		}

		if workflowManager.HasWorkflow(spec.ID) {
			http.Error(w, "workflow already loaded; activate another workflow first", http.StatusConflict)
			return
		}
		if _, err := resolveWorkflowSpecPathByID(spec.ID); err == nil {
			http.Error(w, "workflow id already exists", http.StatusConflict)
			return
		}

		if err := writeUploadedWorkflow(spec.ID, prettyJSON, cppHeaders); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := ensureWorkflowWasmArtifacts(spec, uploadedWorkflowSpecPath(spec.ID)); err != nil {
			http.Error(w, fmt.Sprintf("workflow uploaded but wasm build failed: %v", err), http.StatusBadRequest)
			return
		}

		writeJSON(w, map[string]any{
			"workflow_id": spec.ID,
			"uploaded":    true,
			"path":        uploadedWorkflowSpecPath(spec.ID),
		}, http.StatusCreated)
	}
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

		resp, err := activateWorkflow(r.Context(), req.WorkflowID, req.ResetState, engine, workflowManager, store)
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

func activateWorkflow(
	ctx context.Context,
	workflowID string,
	resetState bool,
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

	current := workflowManager.WorkflowIDs()
	for _, id := range current {
		workflowManager.DeleteWorkflow(id)
		engine.RemoveWorkflowJobs(id)
	}
	if resetState {
		if err := store.DeleteWorkflowState(ctx, workflowID); err != nil {
			return adminWorkflowActivateResponse{}, fmt.Errorf("failed to reset workflow state: %w", err)
		}
	}

	spec, result, recovered, err := loadWorkflowFromPath(ctx, path, engine, workflowManager, store)
	if err != nil {
		return adminWorkflowActivateResponse{}, err
	}
	if err := store.SetActiveWorkflowID(ctx, spec.ID); err != nil {
		log.Printf("failed to persist active workflow id: %v", err)
	}

	return adminWorkflowActivateResponse{
		WorkflowID:      spec.ID,
		ResetState:      resetState,
		RecoveredNodes:  recovered,
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

	workflowManager.DeleteWorkflow(workflowID)
	engine.RemoveWorkflowJobs(workflowID)

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
	spec, err := readWorkflowSpecFromPath(path)
	if err != nil {
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}
	normalized, err := scheduler.ValidateWorkflowSpec(spec)
	if err != nil {
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}
	if err := ensureWorkflowWasmArtifacts(normalized, path); err != nil {
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}
	if workflowManager.HasWorkflow(normalized.ID) {
		return normalized, scheduler.WorkflowLoadResult{}, 0, fmt.Errorf("workflow already loaded: %s", normalized.ID)
	}

	completedOutputs, err := store.LoadWorkflowCompletedOutputs(ctx, normalized.ID)
	if err != nil {
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}

	result, jobs, err := workflowManager.LoadWorkflowWithCompleted(normalized, completedOutputs)
	if err != nil {
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}
	if err := enqueueWorkflowJobs(engine, jobs); err != nil {
		workflowManager.DeleteWorkflow(result.WorkflowID)
		return scheduler.WorkflowSpec{}, scheduler.WorkflowLoadResult{}, 0, err
	}

	return normalized, result, len(completedOutputs), nil
}

func readWorkflowSpecFromPath(path string) (scheduler.WorkflowSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return scheduler.WorkflowSpec{}, err
	}
	defer file.Close()

	var spec scheduler.WorkflowSpec
	if err := json.NewDecoder(file).Decode(&spec); err != nil {
		return scheduler.WorkflowSpec{}, err
	}
	return spec, nil
}

func requiredFilePart(r *http.Request, key string) (*multipart.FileHeader, error) {
	if r.MultipartForm == nil {
		return nil, fmt.Errorf("multipart form is missing")
	}
	files := r.MultipartForm.File[key]
	if len(files) == 0 {
		return nil, fmt.Errorf("missing multipart file: %s", key)
	}
	return files[0], nil
}

func parseAndNormalizeWorkflowJSON(fileHeader *multipart.FileHeader) (scheduler.WorkflowSpec, []byte, error) {
	src, err := fileHeader.Open()
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}
	defer src.Close()

	raw, err := io.ReadAll(io.LimitReader(src, 4<<20))
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}

	var spec scheduler.WorkflowSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return scheduler.WorkflowSpec{}, nil, fmt.Errorf("workflow json decode failed: %w", err)
	}

	normalized, err := scheduler.ValidateWorkflowSpec(spec)
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, fmt.Errorf("workflow validation failed: %w", err)
	}

	pretty, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, err
	}
	return normalized, pretty, nil
}

func writeUploadedWorkflow(workflowID string, workflowJSON []byte, cppHeaders []*multipart.FileHeader) error {
	workflowDir := uploadedWorkflowDir(workflowID)
	cppDir := filepath.Join(workflowDir, uploadedCPPDirName)

	if err := os.MkdirAll(cppDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(uploadedWorkflowSpecPath(workflowID), workflowJSON, 0o644); err != nil {
		return err
	}

	for _, header := range cppHeaders {
		filename, err := sanitizeUploadFilename(header.Filename, ".cpp")
		if err != nil {
			return fmt.Errorf("invalid cpp file %q: %w", header.Filename, err)
		}
		if err := writeMultipartFile(header, filepath.Join(cppDir, filename)); err != nil {
			return err
		}
	}

	return nil
}

func sanitizeUploadFilename(name, requiredExt string) (string, error) {
	base := strings.TrimSpace(filepath.Base(name))
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("filename is empty")
	}
	ext := strings.ToLower(filepath.Ext(base))
	if ext != strings.ToLower(requiredExt) {
		return "", fmt.Errorf("expected %s extension", requiredExt)
	}
	return base, nil
}

func writeMultipartFile(header *multipart.FileHeader, dstPath string) error {
	src, err := header.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, io.LimitReader(src, 64<<20)); err != nil {
		return err
	}
	return nil
}

func discoverUploadedWorkflowIDs() ([]string, error) {
	index, err := discoverWorkflowSpecIndex()
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func discoverWorkflowIDs() ([]string, error) {
	return discoverUploadedWorkflowIDs()
}

func resolveWorkflowSpecPathByID(workflowID string) (string, error) {
	index, err := discoverWorkflowSpecIndex()
	if err != nil {
		return "", err
	}
	path, ok := index[strings.TrimSpace(workflowID)]
	if !ok {
		return "", os.ErrNotExist
	}
	return path, nil
}

func discoverWorkflowSpecIndex() (map[string]string, error) {
	index := map[string]string{}

	if _, err := os.Stat(workflowsRootDir); err != nil {
		if os.IsNotExist(err) {
			return index, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(workflowsRootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".json" {
			return nil
		}

		spec, err := readWorkflowSpecFromPath(path)
		if err != nil {
			return nil
		}
		normalized, err := scheduler.ValidateWorkflowSpec(spec)
		if err != nil {
			return nil
		}
		if !isSafeWorkflowID(normalized.ID) {
			return nil
		}

		if existing, exists := index[normalized.ID]; exists {
			if strings.Contains(existing, string(filepath.Separator)+"uploaded"+string(filepath.Separator)) {
				return nil
			}
		}
		index[normalized.ID] = path
		return nil
	})
	if err != nil {
		return nil, err
	}

	return index, nil
}

func ensureWorkflowWasmArtifacts(spec scheduler.WorkflowSpec, specPath string) error {
	specPath = strings.TrimSpace(specPath)
	if specPath == "" {
		return fmt.Errorf("workflow spec path is required")
	}

	sourceMap, err := discoverCPPSourceMap(filepath.Dir(specPath))
	if err != nil {
		return err
	}

	built := map[string]bool{}
	for _, node := range spec.Nodes {
		relOut, err := wasmRelativePathFromURL(node.WasmURL)
		if err != nil {
			return fmt.Errorf("node %s wasm_url invalid: %w", node.ID, err)
		}
		if built[relOut] {
			continue
		}
		sourcePath, err := sourceForWasmOutput(relOut, sourceMap)
		if err != nil {
			return fmt.Errorf("node %s: %w", node.ID, err)
		}
		outputPath := filepath.Join("static", filepath.FromSlash(relOut))
		if err := compileCPPToWasmIfNeeded(sourcePath, outputPath); err != nil {
			return fmt.Errorf("node %s compile failed: %w", node.ID, err)
		}
		built[relOut] = true
	}
	return nil
}

func discoverCPPSourceMap(baseDir string) (map[string]string, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("source base dir is required")
	}

	candidates := []string{
		baseDir,
		filepath.Join(baseDir, uploadedCPPDirName),
	}
	out := map[string]string{}

	for _, dir := range candidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := strings.TrimSpace(entry.Name())
			if strings.ToLower(filepath.Ext(name)) != ".cpp" {
				continue
			}
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			if stem == "" {
				continue
			}
			if _, exists := out[stem]; exists {
				continue
			}
			out[stem] = filepath.Join(dir, name)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no .cpp sources found under %s", baseDir)
	}
	return out, nil
}

func wasmRelativePathFromURL(wasmURL string) (string, error) {
	wasmURL = strings.TrimSpace(wasmURL)
	if wasmURL == "" {
		return "", fmt.Errorf("wasm_url is empty")
	}

	base := wasmURL
	if idx := strings.Index(base, "?"); idx >= 0 {
		base = base[:idx]
	}
	if idx := strings.Index(base, "#"); idx >= 0 {
		base = base[:idx]
	}
	base = strings.TrimSpace(base)
	if base == "" || !strings.HasPrefix(base, "/") {
		return "", fmt.Errorf("wasm_url must start with /")
	}
	if strings.Contains(base, "..") {
		return "", fmt.Errorf("wasm_url must not contain ..")
	}
	if strings.ToLower(filepath.Ext(base)) != ".wasm" {
		return "", fmt.Errorf("wasm_url must point to .wasm")
	}
	return strings.TrimPrefix(base, "/"), nil
}

func sourceForWasmOutput(relOut string, sourceMap map[string]string) (string, error) {
	stem := strings.TrimSuffix(filepath.Base(relOut), filepath.Ext(relOut))
	sourcePath, ok := sourceMap[stem]
	if !ok {
		return "", fmt.Errorf("missing source %s.cpp for wasm %s", stem, relOut)
	}
	return sourcePath, nil
}

func compileCPPToWasmIfNeeded(sourcePath, outputPath string) error {
	srcInfo, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if outInfo, err := os.Stat(outputPath); err == nil {
		if !srcInfo.ModTime().After(outInfo.ModTime()) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}

	cmd := exec.Command(
		"emcc",
		sourcePath,
		"-O3",
		"-s", "STANDALONE_WASM=1",
		"-s", "ALLOW_MEMORY_GROWTH=1",
		"-s", "INITIAL_MEMORY=67108864",
		"--no-entry",
		"-Wl,--export-all",
		"-o", outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func uploadedWorkflowDir(workflowID string) string {
	return filepath.Join(uploadedWorkflowsDir, workflowID)
}

func uploadedWorkflowSpecPath(workflowID string) string {
	return filepath.Join(uploadedWorkflowDir(workflowID), uploadedWorkflowFile)
}

func pathInsideDir(path string, root string) (bool, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false, nil
	}
	return true, nil
}

func isSafeWorkflowID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
