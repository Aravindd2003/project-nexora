package model

import (
	"encoding/json"
	"time"
)

// Status values form a strictly unidirectional state machine.
// See docs/architecture.md for the full transition diagram.
const (
	StatusPending      = "PENDING"
	StatusQueued       = "QUEUED"
	StatusRunning      = "RUNNING"
	StatusRetrying     = "RETRYING"
	StatusSucceeded    = "SUCCEEDED"
	StatusFailed       = "FAILED"
	StatusDeadLettered = "DEAD_LETTERED"
	StatusCancelled    = "CANCELLED"
)

// terminalStatuses cannot be transitioned out of (Terminal Immutability).
var terminalStatuses = map[string]bool{
	StatusSucceeded:    true,
	StatusDeadLettered: true,
	StatusCancelled:    true,
}

func IsTerminal(status string) bool {
	return terminalStatuses[status]
}

type Operation struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	IdempotencyKey *string         `json:"idempotency_key,omitempty"`
	OpType         string          `json:"op_type"`
	Status         string          `json:"status"`
	Input          json.RawMessage `json:"input"`
	Output         json.RawMessage `json:"output,omitempty"`
	ErrorMessage   *string         `json:"error_message,omitempty"`
	AttemptCount   int             `json:"attempt_count"`
	MaxRetries     int             `json:"max_retries"`
	CreatedAt      time.Time       `json:"created_at"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}

type Attempt struct {
	ID             string     `json:"id"`
	OperationID    string     `json:"operation_id"`
	AttemptNumber  int        `json:"attempt_number"`
	WorkerID       *string    `json:"worker_id,omitempty"`
	ProviderUsed   *string    `json:"provider_used,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Success        *bool      `json:"success,omitempty"`
	ErrorCategory  *string    `json:"error_category,omitempty"`
	ErrorDetail    *string    `json:"error_detail,omitempty"`
	LatencyMs      *int       `json:"latency_ms,omitempty"`
}
