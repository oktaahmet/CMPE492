package server

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
