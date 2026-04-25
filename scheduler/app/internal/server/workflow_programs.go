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

	prefix := ""
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
		if prefix == "" {
			var err error
			prefix, err = workflowWasmURLPrefixFromSpecPath(specPath)
			if err != nil {
				return scheduler.WorkflowSpec{}, err
			}
		}
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
	rel, err := filepath.Rel(workflowsRootDir, filepath.Dir(strings.TrimSpace(specPath)))
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("workflow spec path is required")
	}
	return "/" + filepath.ToSlash(filepath.Clean(rel)) + "/", nil
}
