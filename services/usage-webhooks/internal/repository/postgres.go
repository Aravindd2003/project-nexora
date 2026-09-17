package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nexora/usage-webhooks/internal/model"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// RecordUsage is idempotent via the UNIQUE(operation_id) constraint: if the
// worker's completion callback is delivered twice (at-least-once delivery
// semantics), the second insert is a no-op rather than double-billing.
func (r *Repository) RecordUsage(ctx context.Context, tenantID, operationID string, units int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO usage_events (tenant_id, operation_id, units)
		VALUES ($1, $2, $3)
		ON CONFLICT (operation_id) DO NOTHING`, tenantID, operationID, units)
	return err
}

func (r *Repository) GetUsageSummary(ctx context.Context, tenantID string, monthlyQuota int64) (*model.UsageSummary, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE true) AS total,
			COALESCE(SUM(units), 0) AS units_consumed
		FROM usage_events
		WHERE tenant_id = $1 AND recorded_at >= date_trunc('month', now())`, tenantID)

	var summary model.UsageSummary
	if err := row.Scan(&summary.TotalOperations, &summary.UnitsConsumed); err != nil {
		return nil, err
	}
	summary.MonthlyQuota = monthlyQuota
	summary.QuotaRemaining = monthlyQuota - summary.UnitsConsumed
	if summary.QuotaRemaining < 0 {
		summary.QuotaRemaining = 0
	}
	return &summary, nil
}

func (r *Repository) CreateWebhookEndpoint(ctx context.Context, tenantID, url string) (*model.WebhookEndpoint, string, error) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, "", fmt.Errorf("generate webhook secret: %w", err)
	}
	secret := hex.EncodeToString(secretBytes)

	row := r.pool.QueryRow(ctx, `
		INSERT INTO webhook_endpoints (tenant_id, url, secret)
		VALUES ($1, $2, $3)
		RETURNING id, tenant_id, url, is_active, created_at`, tenantID, url, secret)

	var w model.WebhookEndpoint
	if err := row.Scan(&w.ID, &w.TenantID, &w.URL, &w.IsActive, &w.CreatedAt); err != nil {
		return nil, "", err
	}
	return &w, secret, nil
}

func (r *Repository) ListWebhookEndpoints(ctx context.Context, tenantID string) ([]model.WebhookEndpoint, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, url, is_active, created_at
		FROM webhook_endpoints WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.WebhookEndpoint
	for rows.Next() {
		var w model.WebhookEndpoint
		if err := rows.Scan(&w.ID, &w.TenantID, &w.URL, &w.IsActive, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ActiveWebhooksForTenant + secret, used internally by the dispatcher.
type endpointWithSecret struct {
	model.WebhookEndpoint
	Secret string
}

func (r *Repository) ActiveWebhooksForTenant(ctx context.Context, tenantID string) ([]endpointWithSecret, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, url, secret, is_active, created_at
		FROM webhook_endpoints WHERE tenant_id = $1 AND is_active = true`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []endpointWithSecret
	for rows.Next() {
		var e endpointWithSecret
		if err := rows.Scan(&e.ID, &e.TenantID, &e.URL, &e.Secret, &e.IsActive, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) CreateDelivery(ctx context.Context, webhookID, operationID, eventType string, payload []byte) (string, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO webhook_deliveries (webhook_id, operation_id, event_type, payload, next_attempt_at)
		VALUES ($1, $2, $3, $4, now())
		RETURNING id`, webhookID, operationID, eventType, payload)
	var id string
	return id, row.Scan(&id)
}

func (r *Repository) MarkDeliverySuccess(ctx context.Context, deliveryID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries SET status = 'DELIVERED', last_attempt_at = now(),
		       attempt_count = attempt_count + 1
		WHERE id = $1`, deliveryID)
	return err
}

func (r *Repository) MarkDeliveryFailedAndReschedule(ctx context.Context, deliveryID string, nextAttempt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries SET status = 'PENDING', last_attempt_at = now(),
		       next_attempt_at = $2, attempt_count = attempt_count + 1
		WHERE id = $1`, deliveryID, nextAttempt)
	return err
}

func (r *Repository) MarkDeliveryPermanentlyFailed(ctx context.Context, deliveryID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries SET status = 'FAILED', last_attempt_at = now(),
		       attempt_count = attempt_count + 1
		WHERE id = $1`, deliveryID)
	return err
}

// DuePendingDeliveries powers the retry worker loop's status view (unused
// directly by RetryDue below, kept for potential admin inspection).
func (r *Repository) DuePendingDeliveries(ctx context.Context, limit int) ([]model.WebhookDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, webhook_id, operation_id, event_type, status, attempt_count, last_attempt_at, created_at
		FROM webhook_deliveries
		WHERE status = 'PENDING' AND next_attempt_at <= now()
		ORDER BY next_attempt_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.WebhookDelivery
	for rows.Next() {
		var d model.WebhookDelivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.OperationID, &d.EventType, &d.Status, &d.AttemptCount, &d.LastAttemptAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DueDelivery is the shape the retry sweep actually needs: enough to
// re-send the exact original payload against the endpoint's current
// URL/secret, without the caller needing a second lookup.
type DueDelivery struct {
	ID           string
	URL          string
	Secret       string
	Payload      []byte
	AttemptCount int
}

// DueDeliveriesWithEndpoint joins pending, due deliveries with their
// webhook endpoint in one query, since the retry sweep needs both the
// stored payload and where/how to send it.
func (r *Repository) DueDeliveriesWithEndpoint(ctx context.Context, limit int) ([]DueDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, e.url, e.secret, d.payload, d.attempt_count
		FROM webhook_deliveries d
		JOIN webhook_endpoints e ON e.id = d.webhook_id
		WHERE d.status = 'PENDING' AND d.next_attempt_at <= now()
		ORDER BY d.next_attempt_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DueDelivery
	for rows.Next() {
		var d DueDelivery
		if err := rows.Scan(&d.ID, &d.URL, &d.Secret, &d.Payload, &d.AttemptCount); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
