package model

import "time"

type UsageEvent struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	OperationID string    `json:"operation_id"`
	Units       int       `json:"units"`
	RecordedAt  time.Time `json:"recorded_at"`
}

type UsageSummary struct {
	TotalOperations     int   `json:"total_operations"`
	SucceededOperations int   `json:"succeeded_operations"`
	FailedOperations    int   `json:"failed_operations"`
	UnitsConsumed       int64 `json:"units_consumed"`
	MonthlyQuota        int64 `json:"monthly_quota"`
	QuotaRemaining      int64 `json:"quota_remaining"`
}

type WebhookEndpoint struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	URL       string    `json:"url"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type WebhookDelivery struct {
	ID            string     `json:"id"`
	WebhookID     string     `json:"webhook_id"`
	OperationID   string     `json:"operation_id"`
	EventType     string     `json:"event_type"`
	Status        string     `json:"status"` // PENDING | DELIVERED | FAILED
	AttemptCount  int        `json:"attempt_count"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}
