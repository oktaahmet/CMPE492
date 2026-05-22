package server

import (
	"os"
	"path/filepath"
	"testing"

	"x402-scheduler/internal/scheduler"
)

func TestSanitizeUploadFilename(t *testing.T) {
	t.Parallel()

	got, err := sanitizeUploadFilename(`..\good_name-1.cpp`, ".cpp")
	if err != nil {
		t.Fatalf("sanitizeUploadFilename() error = %v", err)
	}
	if got != "good_name-1.cpp" {
		t.Fatalf("unexpected sanitized filename: %q", got)
	}

	for _, name := range []string{
		"",
		"bad.txt",
		"bad name.cpp",
		".cpp",
	} {
		if _, err := sanitizeUploadFilename(name, ".cpp"); err == nil {
			t.Fatalf("expected filename %q to be rejected", name)
		}
	}
}

func TestSanitizeWorkflowInputFilename(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"load_profile.csv",
		"input-1.json",
		"matrix.v2.txt",
	} {
		got, err := sanitizeWorkflowInputFilename(name)
		if err != nil {
			t.Fatalf("sanitizeWorkflowInputFilename(%q) error = %v", name, err)
		}
		if got != name {
			t.Fatalf("unexpected sanitized filename: %q", got)
		}
	}

	for _, name := range []string{
		"",
		"..",
		"../profile.csv",
		`data\profile.csv`,
		"bad name.csv",
	} {
		if _, err := sanitizeWorkflowInputFilename(name); err == nil {
			t.Fatalf("expected workflow input filename %q to be rejected", name)
		}
	}
}

func TestWasmRelativePathFromURL(t *testing.T) {
	t.Parallel()

	got, err := wasmRelativePathFromURL("/uploaded/demo/node.wasm?v=1#hash")
	if err != nil {
		t.Fatalf("wasmRelativePathFromURL() error = %v", err)
	}
	if got != "uploaded/demo/node.wasm" {
		t.Fatalf("unexpected wasm relative path: %q", got)
	}

	for _, raw := range []string{
		"node.wasm",
		"/../node.wasm",
		"/uploaded/demo/%2e%2e/node.wasm",
		"/uploaded//demo/node.wasm",
		"/uploaded/demo/node.wasm%00",
		"/node.js",
		"",
	} {
		if _, err := wasmRelativePathFromURL(raw); err == nil {
			t.Fatalf("expected wasm url %q to be rejected", raw)
		}
	}
}

func TestDiscoverCPPSourceMapRejectsDuplicateStem(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "node.cpp"), []byte("root"), 0o644); err != nil {
		t.Fatalf("write root source: %v", err)
	}
	cppDir := filepath.Join(root, uploadedCPPDirName)
	if err := os.MkdirAll(cppDir, 0o755); err != nil {
		t.Fatalf("mkdir cpp dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cppDir, "node.cpp"), []byte("uploaded"), 0o644); err != nil {
		t.Fatalf("write uploaded source: %v", err)
	}

	if _, err := discoverCPPSourceMap(root); err == nil {
		t.Fatal("expected duplicate source stem to fail")
	}
}

func TestResolveWorkflowProgramsDerivesBundledAndUploadedWasmURLs(t *testing.T) {
	t.Parallel()

	bundled, err := resolveWorkflowPrograms(scheduler.WorkflowSpec{
		ID: "wf-metrics-review-pipeline",
		Nodes: []scheduler.WorkflowNode{
			{ID: "collect-metrics", Program: "collect_metrics.cpp", RewardUSDC: "0.01"},
		},
	}, filepath.Join("workflows", "metrics-review-pipeline", "metrics-review-pipeline.json"))
	if err != nil {
		t.Fatalf("resolve bundled program: %v", err)
	}
	if got := bundled.Nodes[0].WasmURL; got != "/metrics-review-pipeline/collect_metrics.wasm?v=1" {
		t.Fatalf("unexpected bundled wasm url: %q", got)
	}

	uploaded, err := resolveWorkflowPrograms(scheduler.WorkflowSpec{
		ID: "wf-uploaded-demo",
		Nodes: []scheduler.WorkflowNode{
			{ID: "node-a", Program: "node_a.cpp", RewardUSDC: "0.01"},
		},
	}, uploadedWorkflowSpecPath("wf-uploaded-demo"))
	if err != nil {
		t.Fatalf("resolve uploaded program: %v", err)
	}
	if got := uploaded.Nodes[0].WasmURL; got != "/uploaded/wf-uploaded-demo/node_a.wasm?v=1" {
		t.Fatalf("unexpected uploaded wasm url: %q", got)
	}
}

func TestResolveWorkflowProgramsRejectsUnsafeProgram(t *testing.T) {
	t.Parallel()

	_, err := resolveWorkflowPrograms(scheduler.WorkflowSpec{
		ID: "wf-bad-program",
		Nodes: []scheduler.WorkflowNode{
			{
				ID:         "node-a",
				Program:    "../node_a.cpp",
				RewardUSDC: "0.01",
			},
		},
	}, filepath.Join("workflows", "metrics-review-pipeline", "metrics-review-pipeline.json"))
	if err == nil {
		t.Fatalf("expected unsafe program to fail")
	}
}

func TestResolveWorkflowProgramsDoesNotNeedSpecPathWhenWasmURLIsExplicit(t *testing.T) {
	t.Parallel()

	resolved, err := resolveWorkflowPrograms(scheduler.WorkflowSpec{
		ID: "wf-explicit-wasm",
		Nodes: []scheduler.WorkflowNode{
			{ID: "node-a", Program: "node_a.cpp", WasmURL: "/custom/node_a.wasm", RewardUSDC: "0.01"},
		},
	}, "")
	if err != nil {
		t.Fatalf("resolve explicit wasm url: %v", err)
	}
	if got := resolved.Nodes[0].WasmURL; got != "/custom/node_a.wasm" {
		t.Fatalf("explicit wasm_url should be preserved, got %q", got)
	}
}

func TestWorkflowWasmURLPrefixFromSpecPathRejectsInvalidPath(t *testing.T) {
	t.Parallel()

	if _, err := workflowWasmURLPrefixFromSpecPath(""); err == nil {
		t.Fatal("expected empty spec path to fail")
	}
	if _, err := workflowWasmURLPrefixFromSpecPath(filepath.Join("outside", "workflow.json")); err == nil {
		t.Fatal("expected path outside workflows root to fail")
	}
}

func TestDiscoverWorkflowSpecIndexPrefersUploadedWorkflow(t *testing.T) {
	root := t.TempDir()
	writeWorkflowSpec(t, filepath.Join(root, "bundled", "workflow.json"), "same-id", "/bundled/node.wasm")
	writeWorkflowSpec(t, filepath.Join(root, "uploaded", "same-id", "workflow.json"), "same-id", "/uploaded/same-id/node.wasm")

	index, err := discoverWorkflowSpecIndexUnder(root)
	if err != nil {
		t.Fatalf("discoverWorkflowSpecIndexUnder() error = %v", err)
	}

	got := filepath.Clean(index["same-id"])
	want := filepath.Clean(filepath.Join(root, "uploaded", "same-id", "workflow.json"))
	if got != want {
		t.Fatalf("expected uploaded workflow to win, got %q want %q", got, want)
	}
}

func TestDiscoverWorkflowSpecIndexRejectsDuplicateSamePriorityIDs(t *testing.T) {
	root := t.TempDir()
	writeWorkflowSpec(t, filepath.Join(root, "bundled-a", "workflow.json"), "same-id", "/bundled-a/node.wasm")
	writeWorkflowSpec(t, filepath.Join(root, "bundled-b", "workflow.json"), "same-id", "/bundled-b/node.wasm")

	if _, err := discoverWorkflowSpecIndexUnder(root); err == nil {
		t.Fatal("expected duplicate bundled workflow ids to fail")
	}
}

func TestIsUploadedWorkflowPathUsesRootBoundary(t *testing.T) {
	root := t.TempDir()
	uploadedPath := filepath.Join(root, "uploaded", "wf", "workflow.json")
	notUploadedPath := filepath.Join(root, "not-uploaded", "wf", "workflow.json")

	if !isUploadedWorkflowPath(root, uploadedPath) {
		t.Fatalf("expected uploaded path to be detected")
	}
	if isUploadedWorkflowPath(root, notUploadedPath) {
		t.Fatalf("expected similarly named directory to stay non-uploaded")
	}
}

func writeWorkflowSpec(t *testing.T, path string, id string, wasmURL string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir workflow dir: %v", err)
	}
	raw := `{"id":"` + id + `","nodes":[{"id":"node","wasm_url":"` + wasmURL + `","reward_usdc":"0.01"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write workflow spec: %v", err)
	}
}
