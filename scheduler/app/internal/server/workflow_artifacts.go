package server

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"x402-scheduler/internal/scheduler"
)

func hydrateWorkflowArtifacts(spec scheduler.WorkflowSpec, specPath string) (scheduler.WorkflowSpec, error) {
	if len(spec.Artifacts) == 0 {
		return spec, nil
	}

	baseDir := filepath.Dir(specPath)
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return scheduler.WorkflowSpec{}, err
	}

	out := spec
	out.Artifacts = make([]scheduler.WorkflowArtifact, 0, len(spec.Artifacts))
	for _, artifact := range spec.Artifacts {
		localPath := filepath.Join(absBase, filepath.FromSlash(artifact.Path))
		absPath, err := filepath.Abs(localPath)
		if err != nil {
			return scheduler.WorkflowSpec{}, fmt.Errorf("artifact %s path invalid: %w", artifact.ID, err)
		}
		inside, err := pathInsideDir(absPath, absBase)
		if err != nil {
			return scheduler.WorkflowSpec{}, err
		}
		if !inside {
			return scheduler.WorkflowSpec{}, fmt.Errorf("artifact %s path escapes workflow folder", artifact.ID)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return scheduler.WorkflowSpec{}, fmt.Errorf("artifact %s not found: %w", artifact.ID, err)
		}
		if info.IsDir() {
			return scheduler.WorkflowSpec{}, fmt.Errorf("artifact %s must be a file", artifact.ID)
		}
		sum, err := sha256File(absPath)
		if err != nil {
			return scheduler.WorkflowSpec{}, fmt.Errorf("artifact %s hash failed: %w", artifact.ID, err)
		}
		contentType := strings.TrimSpace(artifact.ContentType)
		if contentType == "" {
			contentType = mime.TypeByExtension(filepath.Ext(absPath))
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		artifact.LocalPath = absPath
		artifact.Size = info.Size()
		artifact.SHA256 = sum
		artifact.ContentType = contentType
		artifact.URL = workflowArtifactURL(spec.ID, artifact.ID)
		out.Artifacts = append(out.Artifacts, artifact)
	}
	return out, nil
}
