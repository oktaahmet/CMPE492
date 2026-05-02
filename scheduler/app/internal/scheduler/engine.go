package scheduler

import (
	"sync"
	"time"
)

type Config struct {
	AssignmentTTL time.Duration
}

type PaymentProvider interface {
	Transfer(event PaymentEvent) (PaymentReceipt, error)
}

// Engine owns the volatile scheduler state: queued jobs, in-flight worker
// assignments, submitted result rounds, online worker heartbeats, and payment
// events waiting to be persisted/processed.
//
// The durable workflow state lives outside this type. Callers persist finalized
// node outputs and payment snapshots after Engine returns a finalized decision.
type Engine struct {
	mu sync.Mutex

	cfg Config

	// jobs keeps the authoritative per-job state. queue is only an ordered index
	// of candidate job IDs, so finalized or deleted jobs may briefly remain there
	// until compaction removes them.
	jobs  map[string]*jobState
	queue []string

	paymentEvents     []PaymentEvent
	paymentEventsByID map[string]int

	workers       map[string]Worker
	finalizedJobs int

	paymentClient      PaymentProvider
	processingPayments bool
}

// jobState tracks a single replica round. assignments are workers currently
// holding the job; submitted/submittedPayload are completed work that can still
// count toward quorum even if the browser later disconnects.
type jobState struct {
	job Job

	assignments      map[string]time.Time
	submitted        map[string]string
	submittedPayload map[string]map[string]any
	acceptedPayload  map[string]any
	acceptedResult   string
	acceptedWorkers  int
	finalized        bool
}

type Assignment struct {
	JobID        string             `json:"job_id"`
	WorkflowID   string             `json:"workflow_id"`
	NodeID       string             `json:"node_id"`
	WasmURL      string             `json:"wasm_url"`
	Args         []any              `json:"args,omitempty"`
	Dependencies []DependencyRef    `json:"dependencies,omitempty"`
	Artifacts    []WorkflowArtifact `json:"artifacts,omitempty"`
	RewardUSDC   string             `json:"reward_usdc"`
	RequiredRep  int                `json:"required_rep"`
}

type ResultSubmission struct {
	JobID         string         `json:"job_id"`
	WorkerID      string         `json:"worker_id"`
	ResultSig     string         `json:"result_sig"`
	ResultPayload map[string]any `json:"result_payload,omitempty"`
}

type Decision struct {
	JobID            string         `json:"job_id"`
	Finalized        bool           `json:"finalized"`
	AcceptedResult   string         `json:"accepted_result,omitempty"`
	AcceptedPayload  map[string]any `json:"accepted_payload,omitempty"`
	AcceptedWorkers  int            `json:"accepted_workers"`
	RequiredReplicas int            `json:"required_replicas"`
}

type PaymentEvent struct {
	ID           string `json:"id"`
	JobID        string `json:"job_id"`
	WorkflowID   string `json:"workflow_id"`
	AmountUSDC   string `json:"amount_usdc"`
	WorkerID     string `json:"worker_id"`
	AcceptedHash string `json:"accepted_hash"`
	Status       string `json:"status"`
	Attempts     int    `json:"attempts"`
	LastError    string `json:"last_error,omitempty"`
	UpdatedAt    string `json:"updated_at"`
	TxHash       string `json:"tx_hash,omitempty"`
	Network      string `json:"network,omitempty"`
	Payer        string `json:"payer,omitempty"`
}

// Payment status values are persisted in Postgres and exposed through the
// payment-history API, so server code should reuse these constants instead of
// duplicating raw strings.
const (
	PaymentStatusPending    = "pending_x402_transfer"
	PaymentStatusProcessing = "processing_x402_transfer"
	PaymentStatusRetry      = "retry"
	PaymentStatusReview     = "needs_reconciliation"
	PaymentStatusConfirmed  = "confirmed"
)

const (
	paymentStatusPending    = PaymentStatusPending
	paymentStatusProcessing = PaymentStatusProcessing
	paymentStatusRetry      = PaymentStatusRetry
	paymentStatusReview     = PaymentStatusReview
	paymentStatusConfirmed  = PaymentStatusConfirmed
)

type JobRuntimeSnapshot struct {
	JobID            string           `json:"job_id"`
	WorkflowID       string           `json:"workflow_id"`
	NodeID           string           `json:"node_id"`
	AssignedWorkers  []string         `json:"assigned_workers,omitempty"`
	SubmittedWorkers []string         `json:"submitted_workers,omitempty"`
	Finalized        bool             `json:"finalized"`
	AcceptedWorkers  int              `json:"accepted_workers"`
	QueueIndex       int              `json:"queue_index"`
	RequiredReplicas int              `json:"required_replicas"`
	AcceptancePolicy AcceptancePolicy `json:"acceptance_policy,omitempty"`
	Traits           []string         `json:"traits,omitempty"`
}

func NewEngine(cfg Config) *Engine {
	if cfg.AssignmentTTL <= 0 {
		cfg.AssignmentTTL = 60 * time.Second
	}

	return &Engine{
		cfg:               cfg,
		jobs:              make(map[string]*jobState),
		workers:           make(map[string]Worker),
		paymentEventsByID: make(map[string]int),
	}
}
