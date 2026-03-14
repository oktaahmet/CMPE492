package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"x402-scheduler/internal/scheduler"
)

type Store struct {
	db *sql.DB
}

const (
	activeWorkflowMetaKey = "active_workflow_id"
	topologyModeMetaKey   = "topology_mode"
)

func NewStore(dsn string) (*Store, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	stmts := []string{
		`
		CREATE TABLE IF NOT EXISTS workflow_node_state (
			workflow_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			job_id TEXT NOT NULL,
			accepted_result TEXT,
			output JSONB NOT NULL DEFAULT '{}'::jsonb,
			finalized_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (workflow_id, node_id)
		)
		`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_node_state_workflow_id ON workflow_node_state(workflow_id)`,
		`
		CREATE TABLE IF NOT EXISTS payment_events (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			workflow_id TEXT NOT NULL,
			worker_id TEXT NOT NULL,
			amount_usdc TEXT NOT NULL,
			accepted_hash TEXT NOT NULL,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			tx_hash TEXT,
			network TEXT,
			payer TEXT,
			updated_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
		`,
		`CREATE INDEX IF NOT EXISTS idx_payment_events_status ON payment_events(status)`,
		`
		CREATE TABLE IF NOT EXISTS scheduler_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
		`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) UpsertWorkflowNodeCompletion(
	ctx context.Context,
	workflowID string,
	nodeID string,
	jobID string,
	acceptedResult string,
	output map[string]any,
	finalizedAt time.Time,
) error {
	workflowID = strings.TrimSpace(workflowID)
	nodeID = strings.TrimSpace(nodeID)
	jobID = strings.TrimSpace(jobID)
	if workflowID == "" || nodeID == "" || jobID == "" {
		return fmt.Errorf("workflow_id, node_id and job_id are required")
	}
	if output == nil {
		output = map[string]any{}
	}
	if finalizedAt.IsZero() {
		finalizedAt = time.Now().UTC()
	}

	outputJSON, err := json.Marshal(output)
	if err != nil {
		return err
	}

	const q = `
	INSERT INTO workflow_node_state (
		workflow_id, node_id, job_id, accepted_result, output, finalized_at, updated_at
	)
	VALUES ($1, $2, $3, $4, $5::jsonb, $6, NOW())
	ON CONFLICT (workflow_id, node_id) DO UPDATE
	SET
		job_id = EXCLUDED.job_id,
		accepted_result = EXCLUDED.accepted_result,
		output = EXCLUDED.output,
		finalized_at = EXCLUDED.finalized_at,
		updated_at = NOW()
	`
	_, err = s.db.ExecContext(
		ctx,
		q,
		workflowID,
		nodeID,
		jobID,
		acceptedResult,
		string(outputJSON),
		finalizedAt,
	)
	return err
}

func (s *Store) LoadWorkflowCompletedOutputs(ctx context.Context, workflowID string) (map[string]map[string]any, error) {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return map[string]map[string]any{}, nil
	}

	const q = `
	SELECT node_id, output
	FROM workflow_node_state
	WHERE workflow_id = $1
	`
	rows, err := s.db.QueryContext(ctx, q, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]map[string]any{}
	for rows.Next() {
		var nodeID string
		var raw []byte
		if err := rows.Scan(&nodeID, &raw); err != nil {
			return nil, err
		}
		payload := map[string]any{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &payload); err != nil {
				return nil, err
			}
		}
		out[nodeID] = payload
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetActiveWorkflowID(ctx context.Context) (string, error) {
	return s.getMetaValue(ctx, activeWorkflowMetaKey)
}

func (s *Store) SetActiveWorkflowID(ctx context.Context, workflowID string) error {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return nil
	}
	return s.setMetaValue(ctx, activeWorkflowMetaKey, workflowID)
}

func (s *Store) ClearActiveWorkflowID(ctx context.Context) error {
	return s.clearMetaValue(ctx, activeWorkflowMetaKey)
}

func (s *Store) GetTopologyMode(ctx context.Context) (string, error) {
	return s.getMetaValue(ctx, topologyModeMetaKey)
}

func (s *Store) SetTopologyMode(ctx context.Context, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return nil
	}
	return s.setMetaValue(ctx, topologyModeMetaKey, mode)
}

func (s *Store) DeleteWorkflowState(ctx context.Context, workflowID string) error {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return fmt.Errorf("workflow_id is required")
	}

	const q = `DELETE FROM workflow_node_state WHERE workflow_id = $1`
	_, err := s.db.ExecContext(ctx, q, workflowID)
	return err
}

func (s *Store) LoadWorkflowNodeOutput(
	ctx context.Context,
	workflowID string,
	nodeID string,
) (map[string]any, bool, error) {
	workflowID = strings.TrimSpace(workflowID)
	nodeID = strings.TrimSpace(nodeID)
	if workflowID == "" || nodeID == "" {
		return nil, false, nil
	}

	const q = `
	SELECT output
	FROM workflow_node_state
	WHERE workflow_id = $1 AND node_id = $2
	`
	var raw []byte
	if err := s.db.QueryRowContext(ctx, q, workflowID, nodeID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	payload := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, false, err
		}
	}
	return payload, true, nil
}

func (s *Store) UpsertPaymentEvents(ctx context.Context, events []scheduler.PaymentEvent) error {
	if len(events) == 0 {
		return nil
	}

	const q = `
	INSERT INTO payment_events (
		id, job_id, workflow_id, worker_id, amount_usdc, accepted_hash, status,
		attempts, last_error, tx_hash, network, payer, updated_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	ON CONFLICT (id) DO UPDATE
	SET
		job_id = EXCLUDED.job_id,
		workflow_id = EXCLUDED.workflow_id,
		worker_id = EXCLUDED.worker_id,
		amount_usdc = EXCLUDED.amount_usdc,
		accepted_hash = EXCLUDED.accepted_hash,
		status = EXCLUDED.status,
		attempts = EXCLUDED.attempts,
		last_error = EXCLUDED.last_error,
		tx_hash = EXCLUDED.tx_hash,
		network = EXCLUDED.network,
		payer = EXCLUDED.payer,
		updated_at = EXCLUDED.updated_at
	`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	for _, event := range events {
		updatedAt := time.Now().UTC()
		if ts := strings.TrimSpace(event.UpdatedAt); ts != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, ts); parseErr == nil {
				updatedAt = parsed
			}
		}

		if _, err := tx.ExecContext(
			ctx,
			q,
			event.ID,
			event.JobID,
			event.WorkflowID,
			event.WorkerID,
			event.AmountUSDC,
			event.AcceptedHash,
			event.Status,
			event.Attempts,
			event.LastError,
			event.TxHash,
			event.Network,
			event.Payer,
			updatedAt,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

const listPaymentEventsSelect = `
SELECT
	id, job_id, workflow_id, amount_usdc, worker_id, accepted_hash, status,
	attempts, COALESCE(last_error, ''), COALESCE(tx_hash, ''), COALESCE(network, ''),
	COALESCE(payer, ''), updated_at
FROM payment_events
`

func (s *Store) ListPaymentEvents(ctx context.Context, workerID string) ([]scheduler.PaymentEvent, error) {
	workerID = strings.TrimSpace(workerID)

	const qAll = listPaymentEventsSelect + `ORDER BY updated_at DESC, created_at DESC`
	const qByWorker = listPaymentEventsSelect + `WHERE worker_id = $1 ORDER BY updated_at DESC, created_at DESC`

	var (
		rows *sql.Rows
		err  error
	)
	if workerID == "" {
		rows, err = s.db.QueryContext(ctx, qAll)
	} else {
		rows, err = s.db.QueryContext(ctx, qByWorker, workerID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []scheduler.PaymentEvent{}
	for rows.Next() {
		event, scanErr := scanPaymentEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ListPendingPaymentEvents(ctx context.Context) ([]scheduler.PaymentEvent, error) {
	const q = listPaymentEventsSelect + `
	WHERE status = 'pending_x402_transfer' OR status = 'retry'
	ORDER BY updated_at ASC, created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []scheduler.PaymentEvent{}
	for rows.Next() {
		event, scanErr := scanPaymentEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanPaymentEvent(scanner interface {
	Scan(dest ...any) error
}) (scheduler.PaymentEvent, error) {
	var (
		event     scheduler.PaymentEvent
		updatedAt time.Time
	)
	if err := scanner.Scan(
		&event.ID,
		&event.JobID,
		&event.WorkflowID,
		&event.AmountUSDC,
		&event.WorkerID,
		&event.AcceptedHash,
		&event.Status,
		&event.Attempts,
		&event.LastError,
		&event.TxHash,
		&event.Network,
		&event.Payer,
		&updatedAt,
	); err != nil {
		return scheduler.PaymentEvent{}, err
	}
	event.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return event, nil
}

func (s *Store) getMetaValue(ctx context.Context, key string) (string, error) {
	const q = `SELECT value FROM scheduler_meta WHERE key = $1`
	var value string
	if err := s.db.QueryRowContext(ctx, q, key).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (s *Store) setMetaValue(ctx context.Context, key string, value string) error {
	const q = `
	INSERT INTO scheduler_meta (key, value, updated_at)
	VALUES ($1, $2, NOW())
	ON CONFLICT (key) DO UPDATE
	SET value = EXCLUDED.value, updated_at = NOW()
	`
	_, err := s.db.ExecContext(ctx, q, key, value)
	return err
}

func (s *Store) clearMetaValue(ctx context.Context, key string) error {
	const q = `DELETE FROM scheduler_meta WHERE key = $1`
	_, err := s.db.ExecContext(ctx, q, key)
	return err
}
