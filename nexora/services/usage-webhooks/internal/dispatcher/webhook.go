package dispatcher

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/nexora/usage-webhooks/internal/repository"
)

type Dispatcher struct {
	repo   *repository.Repository
	client *http.Client
}

func New(repo *repository.Repository) *Dispatcher {
	return &Dispatcher{
		repo:   repo,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

type eventPayload struct {
	EventType   string          `json:"event_type"`
	OperationID string          `json:"operation_id"`
	TenantID    string          `json:"tenant_id"`
	Timestamp   time.Time       `json:"timestamp"`
	Data        json.RawMessage `json:"data,omitempty"`
}

// Dispatch attempts delivery to every active webhook endpoint for the
// tenant. Failures are recorded with an exponential-backoff next_attempt_at
// rather than retried inline, so a slow/dead customer endpoint never blocks
// the caller (Service 2 reporting an operation outcome). The exact signed
// body is persisted alongside the delivery record so a later retry (see
// RetryDue) resends byte-for-byte identical content rather than
// reconstructing — and potentially subtly changing — the payload.
func (d *Dispatcher) Dispatch(ctx context.Context, tenantID, operationID, eventType string, data json.RawMessage) error {
	endpoints, err := d.repo.ActiveWebhooksForTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("load webhook endpoints: %w", err)
	}

	for _, ep := range endpoints {
		body, err := json.Marshal(eventPayload{
			EventType:   eventType,
			OperationID: operationID,
			TenantID:    tenantID,
			Timestamp:   time.Now().UTC(),
			Data:        data,
		})
		if err != nil {
			continue
		}

		deliveryID, err := d.repo.CreateDelivery(ctx, ep.ID, operationID, eventType, body)
		if err != nil {
			continue // don't let one bad insert block other endpoints
		}
		d.attemptDelivery(ctx, deliveryID, ep.URL, ep.Secret, body, 1)
	}
	return nil
}

// RetryDue is called by a background loop in Service 3 to sweep pending
// deliveries whose backoff window has elapsed, and actually resends each
// one against its endpoint's current URL/secret using the exact payload
// bytes stored at creation time.
func (d *Dispatcher) RetryDue(ctx context.Context) (int, error) {
	due, err := d.repo.DueDeliveriesWithEndpoint(ctx, 50)
	if err != nil {
		return 0, err
	}
	for _, item := range due {
		d.attemptDelivery(ctx, item.ID, item.URL, item.Secret, item.Payload, item.AttemptCount+1)
	}
	return len(due), nil
}

func (d *Dispatcher) attemptDelivery(ctx context.Context, deliveryID, url, secret string, body []byte, attempt int) {
	signature := sign(secret, body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		_ = d.repo.MarkDeliveryPermanentlyFailed(ctx, deliveryID)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nexora-Signature", signature)
	req.Header.Set("X-Nexora-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	resp, err := d.client.Do(req)
	if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		resp.Body.Close()
		_ = d.repo.MarkDeliverySuccess(ctx, deliveryID)
		return
	}
	if resp != nil {
		resp.Body.Close()
	}

	const maxAttempts = 5
	if attempt >= maxAttempts {
		_ = d.repo.MarkDeliveryPermanentlyFailed(ctx, deliveryID)
		return
	}
	backoff := time.Duration(attempt*attempt) * 5 * time.Second // 5s, 20s, 45s, 80s...
	_ = d.repo.MarkDeliveryFailedAndReschedule(ctx, deliveryID, time.Now().Add(backoff))
}

// sign implements the bonus HMAC-SHA256 webhook signing: the customer can
// recompute this over the raw body with the shared secret to verify
// authenticity and reject replayed/tampered payloads.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
