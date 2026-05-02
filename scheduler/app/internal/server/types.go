package server

import (
	"context"

	"x402-scheduler/internal/scheduler"
)

type RegisterWorkerRequest struct {
	WorkerID string `json:"worker_id"`
}

type RequeuePaymentsResponse struct {
	RequeuedCount int `json:"requeued_count"`
}

type NodeOutputChunkResponse struct {
	Mode       string `json:"mode"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	NextOffset int    `json:"next_offset,omitempty"`
	Done       bool   `json:"done"`

	TotalItems int    `json:"total_items,omitempty"`
	TotalChars int    `json:"total_chars,omitempty"`
	Items      []any  `json:"items,omitempty"`
	Data       string `json:"data,omitempty"`
}

type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}

type adminWorkflowListResponse struct {
	ActiveWorkflowID string   `json:"active_workflow_id,omitempty"`
	LoadedWorkflowID string   `json:"loaded_workflow_id,omitempty"`
	TopologyMode     string   `json:"topology_mode"`
	UploadedIDs      []string `json:"uploaded_ids"`
}

type adminWorkflowActivateRequest struct {
	WorkflowID   string `json:"workflow_id"`
	ResetState   bool   `json:"reset_state,omitempty"`
	TopologyMode string `json:"topology_mode,omitempty"`
}

type adminWorkflowActivateResponse struct {
	WorkflowID      string `json:"workflow_id"`
	ResetState      bool   `json:"reset_state"`
	TopologyMode    string `json:"topology_mode"`
	RecoveredNodes  int    `json:"recovered_nodes"`
	EnqueuedJobs    int    `json:"enqueued_jobs"`
	TopologicalSize int    `json:"topological_size"`
}

type adminWorkflowDeleteRequest struct {
	WorkflowID string `json:"workflow_id"`
}

type adminWorkflowDeleteResponse struct {
	WorkflowID string `json:"workflow_id"`
	Deleted    bool   `json:"deleted"`
}

type adminRuntimeResponse struct {
	ActiveWorkflowID string                             `json:"active_workflow_id,omitempty"`
	LoadedWorkflowID string                             `json:"loaded_workflow_id,omitempty"`
	TopologyMode     string                             `json:"topology_mode"`
	Stats            scheduler.Stats                    `json:"stats"`
	Workflow         *scheduler.WorkflowRuntimeSnapshot `json:"workflow,omitempty"`
	Jobs             []scheduler.JobRuntimeSnapshot     `json:"jobs,omitempty"`
}

// workflowOutputStore keeps workflow output handlers testable without requiring
// a concrete Postgres store.
type workflowOutputStore interface {
	LoadWorkflowNodeOutput(ctx context.Context, workflowID string, nodeID string) (map[string]any, bool, error)
}

// paymentEventStore is the narrow read side needed by the worker payment
// history endpoint.
type paymentEventStore interface {
	ListPaymentEventsForWorker(ctx context.Context, workerID string) ([]scheduler.PaymentEvent, error)
}
