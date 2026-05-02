package scheduler

import "time"

type AcceptancePolicy string
type ExecutionTarget string

const (
	AcceptancePolicyConsensus  AcceptancePolicy = "consensus"
	AcceptancePolicyCollectAll AcceptancePolicy = "collect_all"

	ExecutionTargetBrowserWorker ExecutionTarget = "browser_worker"
	ExecutionTargetServer        ExecutionTarget = "server"
)

func NormalizeAcceptancePolicy(raw string) AcceptancePolicy {
	switch AcceptancePolicy(raw) {
	case AcceptancePolicyCollectAll:
		return AcceptancePolicyCollectAll
	default:
		return AcceptancePolicyConsensus
	}
}

func IsValidAcceptancePolicy(raw string) bool {
	switch AcceptancePolicy(raw) {
	case "", AcceptancePolicyConsensus, AcceptancePolicyCollectAll:
		return true
	default:
		return false
	}
}

func NormalizeExecutionTarget(raw string) ExecutionTarget {
	switch ExecutionTarget(raw) {
	case ExecutionTargetServer:
		return ExecutionTargetServer
	default:
		return ExecutionTargetBrowserWorker
	}
}

func IsValidExecutionTarget(raw string) bool {
	switch ExecutionTarget(raw) {
	case "", ExecutionTargetBrowserWorker, ExecutionTargetServer:
		return true
	default:
		return false
	}
}

type Job struct {
	ID                string                      `json:"id"`
	WorkflowID        string                      `json:"workflow_id"`
	NodeID            string                      `json:"node_id"`
	WasmURL           string                      `json:"wasm_url"`
	ExecutionTarget   ExecutionTarget             `json:"execution_target,omitempty"`
	Args              []any                       `json:"args,omitempty"`
	Dependencies      []DependencyRef             `json:"dependencies,omitempty"`
	Artifacts         []WorkflowArtifact          `json:"artifacts,omitempty"`
	OutputArtifacts   []WorkflowArtifact          `json:"output_artifacts,omitempty"`
	ResultSchema      map[string]PayloadFieldRule `json:"result_schema,omitempty"`
	RewardUSDC        string                      `json:"reward_usdc"`
	ReplicationFactor int                         `json:"replication_factor,omitempty"`
	AcceptancePolicy  AcceptancePolicy            `json:"acceptance_policy,omitempty"`
	Traits            []string                    `json:"traits,omitempty"`
}

type DependencyRef struct {
	WorkflowID string `json:"workflow_id"`
	NodeID     string `json:"node_id"`
}

type WorkflowArtifact struct {
	ID          string `json:"id"`
	File        string `json:"file,omitempty"`
	Path        string `json:"path,omitempty"`
	URL         string `json:"url,omitempty"`
	Size        int64  `json:"size,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	ContentType string `json:"-"`
	LocalPath   string `json:"-"`
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
