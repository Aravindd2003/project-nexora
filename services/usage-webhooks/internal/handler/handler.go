package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nexora/usage-webhooks/internal/dispatcher"
	"github.com/nexora/usage-webhooks/internal/httpx"
	"github.com/nexora/usage-webhooks/internal/repository"
)

type Handler struct {
	repo *repository.Repository
	disp *dispatcher.Dispatcher
}

func New(repo *repository.Repository, disp *dispatcher.Dispatcher) *Handler {
	return &Handler{repo: repo, disp: disp}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	// Public surface (via Gateway)
	r.Get("/v1/usage", h.getUsage)
	r.Post("/v1/webhooks", h.createWebhook)
	r.Get("/v1/webhooks", h.listWebhooks)

	// Internal — called by Service 2 when an operation reaches a terminal state,
	// and by the Gateway's quota guard before admitting a new operation.
	r.Post("/internal/usage/record", h.recordUsage)
	r.Post("/internal/webhooks/dispatch", h.dispatchEvent)
	r.Get("/internal/usage/consumption/{tenantID}", h.getConsumption)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}

func tenantID(r *http.Request) string { return r.Header.Get("X-Tenant-ID") }
func reqID(r *http.Request) string    { return r.Header.Get("X-Request-ID") }

func (h *Handler) getUsage(w http.ResponseWriter, r *http.Request) {
	// Monthly quota is passed by the Gateway (it already fetched tenant
	// limits during auth) to avoid a second cross-service call here.
	var quota int64 = 100000
	if q := r.Header.Get("X-Monthly-Quota"); q != "" {
		json.Unmarshal([]byte(q), &quota)
	}

	summary, err := h.repo.GetUsageSummary(r.Context(), tenantID(r), quota)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load usage", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

type createWebhookReq struct {
	URL string `json:"url"`
}

func (h *Handler) createWebhook(w http.ResponseWriter, r *http.Request) {
	var req createWebhookReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "url is required", reqID(r))
		return
	}
	endpoint, secret, err := h.repo.CreateWebhookEndpoint(r.Context(), tenantID(r), req.URL)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create webhook", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id": endpoint.ID, "url": endpoint.URL, "is_active": endpoint.IsActive,
		"created_at": endpoint.CreatedAt,
		"secret":     secret, // shown once, same pattern as API keys
	})
}

func (h *Handler) listWebhooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := h.repo.ListWebhookEndpoints(r.Context(), tenantID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list webhooks", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"webhooks": hooks})
}

type recordUsageReq struct {
	TenantID    string `json:"tenant_id"`
	OperationID string `json:"operation_id"`
	Units       int    `json:"units"`
}

func (h *Handler) recordUsage(w http.ResponseWriter, r *http.Request) {
	var req recordUsageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error(), reqID(r))
		return
	}
	if err := h.repo.RecordUsage(r.Context(), req.TenantID, req.OperationID, req.Units); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to record usage", reqID(r))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type dispatchEventReq struct {
	TenantID    string          `json:"tenant_id"`
	OperationID string          `json:"operation_id"`
	EventType   string          `json:"event_type"`
	Data        json.RawMessage `json:"data,omitempty"`
}

func (h *Handler) dispatchEvent(w http.ResponseWriter, r *http.Request) {
	var req dispatchEventReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error(), reqID(r))
		return
	}
	if err := h.disp.Dispatch(r.Context(), req.TenantID, req.OperationID, req.EventType, req.Data); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to dispatch webhook", reqID(r))
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// getConsumption is queried by the Gateway's quota guard before it admits a
// new operation — this is what makes "quota exhausted" a hard rejection at
// the perimeter rather than something only visible after the fact on a
// dashboard.
func (h *Handler) getConsumption(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	summary, err := h.repo.GetUsageSummary(r.Context(), tenantID, 0) // quota unused for this call
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load consumption", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"units_consumed": summary.UnitsConsumed})
}
