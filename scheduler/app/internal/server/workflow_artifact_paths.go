package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"x402-scheduler/internal/scheduler"
)

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// hydrateWorkflowArtifacts resolves workflow-authored input artifacts to local
// files and fills in runtime metadata. Paths stay relative to the workflow spec
// directory, so uploaded workflows cannot read arbitrary server files.
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

// workflowArtifactURL addresses static input artifacts by workflow and artifact
// id. Output artifacts include node_id and use outputWorkflowArtifactURL below.
func workflowArtifactURL(workflowID, artifactID string) string {
	q := url.Values{}
	q.Set("workflow_id", workflowID)
	q.Set("artifact_id", artifactID)
	return "/api/workflow/artifact?" + q.Encode()
}

// outputWorkflowArtifactURL points at a finalized server-node artifact. The
// handler still verifies that the node output exists before serving the file.
func outputWorkflowArtifactURL(workflowID, nodeID, artifactID string) string {
	q := url.Values{}
	q.Set("workflow_id", workflowID)
	q.Set("node_id", nodeID)
	q.Set("artifact_id", artifactID)
	return "/api/workflow/artifact?" + q.Encode()
}

// outputArtifactLocalPath maps a server node's declared output file into the
// controlled workflow-data tree and rejects traversal outside that directory.
func outputArtifactLocalPath(workflowID, nodeID, artifactPath string) (string, error) {
	workflowID = strings.TrimSpace(workflowID)
	nodeID = strings.TrimSpace(nodeID)
	artifactPath = strings.TrimSpace(strings.ReplaceAll(artifactPath, "\\", "/"))
	if !isSafeWorkflowID(workflowID) || !isSafeWorkflowID(nodeID) {
		return "", fmt.Errorf("workflow_id or node_id is invalid")
	}
	if artifactPath == "" {
		return "", fmt.Errorf("artifact path is required")
	}

	cleaned := path.Clean(artifactPath)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || path.IsAbs(cleaned) {
		return "", fmt.Errorf("artifact path must be relative and stay inside output artifact folder")
	}

	base := filepath.Join("workflow-data", "artifacts", workflowID, nodeID)
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	localPath := filepath.Join(absBase, filepath.FromSlash(cleaned))
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return "", err
	}
	inside, err := pathInsideDir(absPath, absBase)
	if err != nil {
		return "", err
	}
	if !inside {
		return "", fmt.Errorf("artifact path escapes output artifact folder")
	}
	return absPath, nil
}

// pathInsideDir reports whether candidatePath is inside root after resolving
// both paths to absolute filesystem paths.
func pathInsideDir(candidatePath string, root string) (bool, error) {
	absPath, err := filepath.Abs(candidatePath)
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

// isSafeWorkflowID keeps workflow/node ids usable in URLs and filesystem paths.
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
