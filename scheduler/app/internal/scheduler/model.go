package scheduler

import "time"

type Job struct {
	ID           string                      `json:"id"`
	WorkflowID   string                      `json:"workflow_id"`
	NodeID       string                      `json:"node_id"`
	WasmURL      string                      `json:"wasm_url"`
	Args         []any                       `json:"args,omitempty"`
	ResultSchema map[string]PayloadFieldRule `json:"result_schema,omitempty"`
	RewardUSDC   string                      `json:"reward_usdc"`
}

type PayloadFieldRule struct {
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

type Worker struct {
	ID            string    `json:"id"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	ConnectedAt   time.Time `json:"connected_at"`
}

type Stats struct {
	QueuedJobs      int `json:"queued_jobs"`
	FinalizedJobs   int `json:"finalized_jobs"`
	PendingPayments int `json:"pending_payments"`
	WorkersOnline   int `json:"workers_online"`
	TotalJobs       int `json:"total_jobs"`
}
