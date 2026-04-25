package server

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"x402-scheduler/internal/scheduler"
)

func ensureWorkflowArtifacts(spec scheduler.WorkflowSpec, specPath string) error {
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
		if scheduler.NormalizeExecutionTarget(string(node.ExecutionTarget)) == scheduler.ExecutionTargetServer {
			outputPath, err := nativeExecutablePathFromWasmURL(node.WasmURL)
			if err != nil {
				return fmt.Errorf("node %s native path invalid: %w", node.ID, err)
			}
			if err := compileCPPToNativeIfNeeded(sourcePath, outputPath); err != nil {
				return fmt.Errorf("node %s native compile failed: %w", node.ID, err)
			}
		} else {
			outputPath := filepath.Join("static", filepath.FromSlash(relOut))
			if err := compileCPPToWasmIfNeeded(sourcePath, outputPath); err != nil {
				return fmt.Errorf("node %s compile failed: %w", node.ID, err)
			}
		}
		built[relOut] = true
	}
	return nil
}

func validateUploadedWorkflowWasmURLs(spec scheduler.WorkflowSpec) error {
	expectedPrefix := path.Join("uploaded", spec.ID) + "/"

	for _, node := range spec.Nodes {
		relOut, err := wasmRelativePathFromURL(node.WasmURL)
		if err != nil {
			return fmt.Errorf("node %s wasm_url invalid: %w", node.ID, err)
		}
		if !strings.HasPrefix(relOut, expectedPrefix) {
			return fmt.Errorf(
				"node %s wasm_url must stay under /%s (got /%s)",
				node.ID,
				expectedPrefix,
				relOut,
			)
		}
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
		"-s", "ERROR_ON_UNDEFINED_SYMBOLS=0",
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

func compileCPPToNativeIfNeeded(sourcePath, outputPath string) error {
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

	runnerPath := filepath.Join("workflows", "common", "native_json_runner.cpp")
	cmd := exec.Command(
		"g++",
		sourcePath,
		runnerPath,
		"-std=c++17",
		"-O3",
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
