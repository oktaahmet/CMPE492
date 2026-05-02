package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"x402-scheduler/internal/scheduler"
)

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
	if !isSafeWorkflowID(spec.ID) {
		return scheduler.WorkflowSpec{}, nil, fmt.Errorf("workflow id is invalid")
	}
	spec, err = resolveWorkflowPrograms(spec, uploadedWorkflowSpecPath(spec.ID))
	if err != nil {
		return scheduler.WorkflowSpec{}, nil, fmt.Errorf("workflow program resolution failed: %w", err)
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

// writeUploadedWorkflow writes the normalized workflow spec before its source
// files so later cleanup can remove the whole workflow directory by id.
func writeUploadedWorkflow(workflowID string, workflowJSON []byte, cppHeaders []*multipart.FileHeader, inputHeaders []*multipart.FileHeader) error {
	workflowDir := uploadedWorkflowDir(workflowID)
	cppDir := filepath.Join(workflowDir, uploadedCPPDirName)
	dataDir := filepath.Join(workflowDir, "data")

	if err := os.MkdirAll(cppDir, 0o755); err != nil {
		return err
	}
	if len(inputHeaders) > 0 {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(uploadedWorkflowSpecPath(workflowID), workflowJSON, 0o644); err != nil {
		return err
	}

	seenFilenames := make(map[string]bool, len(cppHeaders))
	for _, header := range cppHeaders {
		filename, err := sanitizeUploadFilename(header.Filename, ".cpp")
		if err != nil {
			return fmt.Errorf("invalid cpp file %q: %w", header.Filename, err)
		}
		if seenFilenames[filename] {
			return fmt.Errorf("duplicate cpp file name: %s", filename)
		}
		seenFilenames[filename] = true
		if err := writeMultipartFileLimited(header, filepath.Join(cppDir, filename), 64<<20); err != nil {
			return err
		}
	}

	seenInputFilenames := make(map[string]bool, len(inputHeaders))
	for _, header := range inputHeaders {
		filename, err := sanitizeWorkflowInputFilename(header.Filename)
		if err != nil {
			return fmt.Errorf("invalid workflow input file %q: %w", header.Filename, err)
		}
		if seenInputFilenames[filename] {
			return fmt.Errorf("duplicate workflow input file name: %s", filename)
		}
		seenInputFilenames[filename] = true
		if err := writeMultipartFileLimited(header, filepath.Join(dataDir, filename), maxWorkflowInputSize); err != nil {
			return fmt.Errorf("workflow input file %s: %w", filename, err)
		}
	}

	invalidateWorkflowSpecIndexCache()
	invalidateWorkflowArtifactSpecCache()
	return nil
}

func sanitizeUploadFilename(name, requiredExt string) (string, error) {
	base := strings.TrimSpace(filepath.Base(name))
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("filename is empty")
	}
	if len(base) > maxUploadFilenameLen {
		return "", fmt.Errorf("filename is too long")
	}
	if strings.IndexByte(base, 0) >= 0 {
		return "", fmt.Errorf("filename contains null byte")
	}
	ext := strings.ToLower(filepath.Ext(base))
	if ext != strings.ToLower(requiredExt) {
		return "", fmt.Errorf("expected %s extension", requiredExt)
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		return "", fmt.Errorf("filename stem is empty")
	}
	for _, r := range stem {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("filename must use letters, numbers, underscore or dash")
	}
	return base, nil
}

func sanitizeWorkflowInputFilename(name string) (string, error) {
	base := strings.TrimSpace(name)
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("filename is empty")
	}
	if len(base) > maxUploadFilenameLen {
		return "", fmt.Errorf("filename is too long")
	}
	if strings.IndexByte(base, 0) >= 0 {
		return "", fmt.Errorf("filename contains null byte")
	}
	if strings.ContainsAny(base, `/\`) {
		return "", fmt.Errorf("filename must not contain path separators")
	}
	if filepath.Base(base) != base {
		return "", fmt.Errorf("filename must be a plain filename")
	}
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", fmt.Errorf("filename must use letters, numbers, dot, underscore or dash")
	}
	return base, nil
}

func writeMultipartFileLimited(header *multipart.FileHeader, dstPath string, maxBytes int64) error {
	src, err := header.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}

	limit := maxBytes + 1
	if limit <= 1 {
		limit = 1
	}
	written, err := io.Copy(dst, io.LimitReader(src, limit))
	if err != nil {
		_ = dst.Close()
		_ = os.Remove(dstPath)
		return err
	}
	if closeErr := dst.Close(); closeErr != nil {
		_ = os.Remove(dstPath)
		return closeErr
	}
	if written > maxBytes {
		_ = os.Remove(dstPath)
		return fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return nil
}

func uploadedWorkflowDir(workflowID string) string {
	return filepath.Join(uploadedWorkflowsDir, workflowID)
}

func uploadedWorkflowSpecPath(workflowID string) string {
	return filepath.Join(uploadedWorkflowDir(workflowID), uploadedWorkflowFile)
}

func cleanupUploadedWorkflowArtifacts(workflowID string) error {
	if strings.TrimSpace(workflowID) == "" {
		return nil
	}
	err := errors.Join(
		os.RemoveAll(uploadedWorkflowDir(workflowID)),
		os.RemoveAll(filepath.Join("static", "uploaded", workflowID)),
	)
	invalidateWorkflowSpecIndexCache()
	invalidateWorkflowArtifactSpecCache()
	return err
}
