package server

import "x402-scheduler/internal/scheduler"

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
