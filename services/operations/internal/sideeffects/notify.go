package sideeffects

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Notifier calls out to Service 3 (Usage & Webhooks) when an operation
// reaches a terminal state. It lives in Service 2 because Service 2 owns
// the state machine — it is the only place that knows authoritatively
// *when* a transition to a terminal state has just happened, so it is the
// natural place to trigger the side effects of that transition rather than
// scattering "did we already notify for this operation" logic elsewhere.
type Notifier struct {
	usageWebhooksURL string
	client           *http.Client
}

func New(usageWebhooksURL string) *Notifier {
	return &Notifier{
		usageWebhooksURL: usageWebhooksURL,
		client:           &http.Client{Timeout: 3 * time.Second},
	}
}

// NotifySuccess records usage and fires the operation.succeeded webhook.
// Failures here are logged, not propagated — a webhook/usage hiccup must
// never flip an already-SUCCEEDED operation back to an error state.
func (n *Notifier) NotifySuccess(ctx context.Context, tenantID, operationID string, output json.RawMessage) {
	n.recordUsage(ctx, tenantID, operationID, 1)
	n.dispatchWebhook(ctx, tenantID, operationID, "operation.succeeded", output)
}

func (n *Notifier) NotifyTerminalFailure(ctx context.Context, tenantID, operationID, status string) {
	eventType := "operation.failed"
	if status == "DEAD_LETTERED" {
		eventType = "operation.dead_lettered"
	}
	n.dispatchWebhook(ctx, tenantID, operationID, eventType, nil)
}

func (n *Notifier) recordUsage(ctx context.Context, tenantID, operationID string, units int) {
	body, _ := json.Marshal(map[string]any{"tenant_id": tenantID, "operation_id": operationID, "units": units})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.usageWebhooksURL+"/internal/usage/record", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		fmt.Printf("operations: usage recording failed for %s: %v\n", operationID, err)
		return
	}
	resp.Body.Close()
}

func (n *Notifier) dispatchWebhook(ctx context.Context, tenantID, operationID, eventType string, data json.RawMessage) {
	body, _ := json.Marshal(map[string]any{
		"tenant_id": tenantID, "operation_id": operationID, "event_type": eventType, "data": data,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.usageWebhooksURL+"/internal/webhooks/dispatch", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		fmt.Printf("operations: webhook dispatch failed for %s: %v\n", operationID, err)
		return
	}
	resp.Body.Close()
}
