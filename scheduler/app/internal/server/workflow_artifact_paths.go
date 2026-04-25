package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
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

func workflowArtifactURL(workflowID, artifactID string) string {
	q := url.Values{}
	q.Set("workflow_id", workflowID)
	q.Set("artifact_id", artifactID)
	return "/api/workflow/artifact?" + q.Encode()
}

func outputWorkflowArtifactURL(workflowID, nodeID, artifactID string) string {
	q := url.Values{}
	q.Set("workflow_id", workflowID)
	q.Set("node_id", nodeID)
	q.Set("artifact_id", artifactID)
	return "/api/workflow/artifact?" + q.Encode()
}

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
