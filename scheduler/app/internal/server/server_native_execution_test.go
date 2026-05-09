package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"x402-scheduler/internal/scheduler"
)

func TestMain(m *testing.M) {
	if os.Getenv("X402_NATIVE_NODE_HELPER") == "1" {
		os.Exit(runNativeNodeHelper())
	}
	os.Exit(m.Run())
}

func TestRunNativeServerNodeFixtureAndOutputArtifacts(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	binaryPath := filepath.Join("native", "wf-native", "server-node"+nativeExecutableExt())
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	if err := copyFile(os.Args[0], binaryPath); err != nil {
		t.Fatalf("copy helper binary: %v", err)
	}

	t.Setenv("X402_NATIVE_NODE_HELPER", "1")

	job := scheduler.Job{
		ID:              "wf-native:server-node",
		WorkflowID:      "wf-native",
		NodeID:          "server-node",
		WasmURL:         "/wf-native/server-node.wasm",
		ExecutionTarget: scheduler.ExecutionTargetServer,
		Args:            []any{map[string]any{"value": float64(41)}},
		OutputArtifacts: []scheduler.WorkflowArtifact{
			{ID: "report", Path: "report.txt"},
		},
		ResultSchema: map[string]scheduler.PayloadFieldRule{
			"output": {Type: "object", Required: true},
		},
	}

	outputArtifacts, err := prepareServerOutputArtifacts(job)
	if err != nil {
		t.Fatalf("prepareServerOutputArtifacts() error = %v", err)
	}
	context := map[string]any{
		"args":             job.Args,
		"inputs":           map[string]any{},
		"artifacts":        map[string]any{},
		"output_artifacts": serverArtifactContext(outputArtifacts),
	}

	resultPayload, err := runNativeServerNode(t.Context(), job, context)
	if err != nil {
		t.Fatalf("runNativeServerNode() error = %v", err)
	}
	output, ok := resultPayload["output"].(map[string]any)
	if !ok || output["value"] != float64(42) {
		t.Fatalf("unexpected native node output: %#v", resultPayload)
	}

	finalized, err := finalizeServerOutputArtifacts(outputArtifacts)
	if err != nil {
		t.Fatalf("finalizeServerOutputArtifacts() error = %v", err)
	}
	if len(finalized) != 1 || finalized[0].Size == 0 || finalized[0].SHA256 == "" {
		t.Fatalf("unexpected finalized artifact metadata: %#v", finalized)
	}
	raw, err := os.ReadFile(finalized[0].LocalPath)
	if err != nil {
		t.Fatalf("read finalized artifact: %v", err)
	}
	if string(raw) != "native helper report\n" {
		t.Fatalf("unexpected artifact content: %q", raw)
	}
}

func runNativeNodeHelper() int {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "read stdin: %v", err)
		return 2
	}
	var input struct {
		OutputArtifacts map[string]struct {
			Path string `json:"path"`
		} `json:"output_artifacts"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "decode input: %v", err)
		return 2
	}
	if artifact, ok := input.OutputArtifacts["report"]; ok && artifact.Path != "" {
		if err := os.WriteFile(artifact.Path, []byte("native helper report\n"), 0o644); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write artifact: %v", err)
			return 2
		}
	}
	_, _ = os.Stdout.WriteString(`{"value":42}`)
	return 0
}

func copyFile(srcPath string, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}
