package server

import (
	"fmt"
	"path/filepath"
	"strings"

	"x402-scheduler/internal/scheduler"
)

func resolveWorkflowPrograms(spec scheduler.WorkflowSpec, specPath string) (scheduler.WorkflowSpec, error) {
	out := spec
	out.Nodes = make([]scheduler.WorkflowNode, len(spec.Nodes))
	copy(out.Nodes, spec.Nodes)

	needsDerivedURL := false
	for _, node := range out.Nodes {
		if strings.TrimSpace(node.Program) != "" && strings.TrimSpace(node.WasmURL) == "" {
			needsDerivedURL = true
			break
		}
	}
	prefix := ""
	if needsDerivedURL {
		var err error
		prefix, err = workflowWasmURLPrefixFromSpecPath(specPath)
		if err != nil {
			return scheduler.WorkflowSpec{}, err
		}
	}

	for i := range out.Nodes {
		program, err := normalizeNodeProgram(out.Nodes[i].Program)
		if err != nil {
			return scheduler.WorkflowSpec{}, fmt.Errorf("node %s program invalid: %w", out.Nodes[i].ID, err)
		}
		out.Nodes[i].Program = program
		if program == "" {
			continue
		}
		if strings.TrimSpace(out.Nodes[i].WasmURL) != "" {
			continue
		}
		// Program names are source filenames; wasm URLs are derived beside the
		// workflow under static/<workflow-dir>/<program-stem>.wasm.
		stem := strings.TrimSuffix(program, filepath.Ext(program))
		out.Nodes[i].WasmURL = prefix + stem + ".wasm?v=1"
	}
	return out, nil
}

func normalizeNodeProgram(raw string) (string, error) {
	program := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if program == "" {
		return "", nil
	}
	if strings.Contains(program, "/") {
		return "", fmt.Errorf("program must be a .cpp filename")
	}
	return sanitizeUploadFilename(program, ".cpp")
}

func workflowWasmURLPrefixFromSpecPath(specPath string) (string, error) {
	specPath = strings.TrimSpace(specPath)
	if specPath == "" {
		return "", fmt.Errorf("workflow spec path is required")
	}
	rel, err := filepath.Rel(workflowsRootDir, filepath.Dir(specPath))
	if err != nil {
		return "", fmt.Errorf("workflow spec path is invalid: %w", err)
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return "", fmt.Errorf("workflow spec path must be inside a workflow directory")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workflow spec path must stay under %s", workflowsRootDir)
	}
	return "/" + filepath.ToSlash(rel) + "/", nil
}
