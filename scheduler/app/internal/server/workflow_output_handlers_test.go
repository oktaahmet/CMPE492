package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeWorkflowOutputStore struct {
	output map[string]any
	found  bool
	err    error
}

func (s fakeWorkflowOutputStore) LoadWorkflowNodeOutput(context.Context, string, string) (map[string]any, bool, error) {
	return s.output, s.found, s.err
}

func TestWorkflowNodeOutputHandlerReturnsStoredOutput(t *testing.T) {
	t.Parallel()

	store := fakeWorkflowOutputStore{
		output: map[string]any{"output": "hello"},
		found:  true,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workflow/node-output?workflow_id=wf&node_id=node", nil)
	rec := httptest.NewRecorder()

	workflowNodeOutputHandler(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if payload["output"] != "hello" {
		t.Fatalf("unexpected output payload: %#v", payload)
	}
}

func TestWorkflowNodeOutputHandlerRejectsMissingAndStoreErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		store fakeWorkflowOutputStore
		want  int
	}{
		{
			name:  "missing",
			store: fakeWorkflowOutputStore{found: false},
			want:  http.StatusNotFound,
		},
		{
			name:  "store_error",
			store: fakeWorkflowOutputStore{err: errors.New("db down")},
			want:  http.StatusInternalServerError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/api/workflow/node-output?workflow_id=wf&node_id=node", nil)
			rec := httptest.NewRecorder()

			workflowNodeOutputHandler(tc.store).ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d body=%s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWorkflowNodeOutputHandlerRejectsUnsafeIDs(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/workflow/node-output?workflow_id=../wf&node_id=node", nil)
	rec := httptest.NewRecorder()

	workflowNodeOutputHandler(fakeWorkflowOutputStore{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected unsafe id bad request, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowNodeOutputChunkHandlerReturnsChunk(t *testing.T) {
	t.Parallel()

	store := fakeWorkflowOutputStore{
		output: map[string]any{"output": []any{float64(1), float64(2), float64(3)}},
		found:  true,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workflow/node-output/chunk?workflow_id=wf&node_id=node&offset=1&limit=1", nil)
	rec := httptest.NewRecorder()

	workflowNodeOutputChunkHandler(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var chunk NodeOutputChunkResponse
	if err := json.NewDecoder(rec.Body).Decode(&chunk); err != nil {
		t.Fatalf("decode chunk: %v", err)
	}
	if chunk.Mode != "array" || chunk.NextOffset != 2 || chunk.TotalItems != 3 || len(chunk.Items) != 1 {
		t.Fatalf("unexpected chunk: %#v", chunk)
	}
}

func TestWorkflowNodeOutputChunkHandlerDefaultsZeroLimit(t *testing.T) {
	t.Parallel()

	store := fakeWorkflowOutputStore{
		output: map[string]any{"output": []any{float64(1), float64(2), float64(3)}},
		found:  true,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workflow/node-output/chunk?workflow_id=wf&node_id=node&limit=0", nil)
	rec := httptest.NewRecorder()

	workflowNodeOutputChunkHandler(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var chunk NodeOutputChunkResponse
	if err := json.NewDecoder(rec.Body).Decode(&chunk); err != nil {
		t.Fatalf("decode chunk: %v", err)
	}
	if chunk.Limit != defaultNodeOutputChunkLimit || len(chunk.Items) != 3 || !chunk.Done {
		t.Fatalf("expected limit=0 to use default, got %#v", chunk)
	}
}

func TestWorkflowNodeOutputChunkHandlerRejectsUnsafeIDs(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/workflow/node-output/chunk?workflow_id=wf&node_id=../node", nil)
	rec := httptest.NewRecorder()

	workflowNodeOutputChunkHandler(fakeWorkflowOutputStore{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected unsafe id bad request, got %d body=%s", rec.Code, rec.Body.String())
	}
}
