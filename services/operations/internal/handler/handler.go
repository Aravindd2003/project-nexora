package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nexora/operations/internal/httpx"
	"github.com/nexora/operations/internal/model"
	"github.com/nexora/operations/internal/queue"
	"github.com/nexora/operations/internal/repository"
	"github.com/nexora/operations/internal/sideeffects"
)

type Handler struct {
	repo   *repository.Repository
	q      *queue.Queue
	notify *sideeffects.Notifier
}

func New(repo *repository.Repository, q *queue.Queue, notify *sideeffects.Notifier) *Handler {
	return &Handler{repo: repo, q: q, notify: notify}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	// Public surface — reached only via the Gateway, which has already
	// authenticated the caller and injects X-Tenant-ID itself. This
	// service never trusts a tenant ID from a request body.
	r.Post("/v1/operations", h.createOperation)
	r.Get("/v1/operations", h.listOperations)
	r.Get("/v1/operations/{id}", h.getOperation)
	r.Post("/v1/operations/{id}/cancel", h.cancelOperation)

	// Internal surface — used by the Worker to report progress, and by the
	// Admin Dashboard (via Gateway) to inspect across tenants.
	r.Post("/internal/operations/{id}/attempts/start", h.startAttempt)
	r.Post("/internal/operations/{id}/attempts/complete", h.completeAttempt)
	r.Get("/internal/operations/{id}", h.adminGetOperation)
	r.Get("/internal/operations/{id}/attempts", h.listAttempts)
	r.Get("/internal/operations", h.adminListOperations)
	r.Get("/internal/summary", h.adminSummary)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}

func tenantID(r *http.Request) string { return r.Header.Get("X-Tenant-ID") }
func reqID(r *http.Request) string    { return r.Header.Get("X-Request-ID") }

type createOperationReq struct {
	OpType string          `json:"op_type"`
	Input  json.RawMessage `json:"input"`
}

func (h *Handler) createOperation(w http.ResponseWriter, r *http.Request) {
	tid := tenantID(r)
	if tid == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "missing_tenant_context", "no tenant context on request", reqID(r))
		return
	}

	var req createOperationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OpType == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "op_type and input are required", reqID(r))
		return
	}

	var idemKey *string
	if k := r.Header.Get("Idempotency-Key"); k != "" {
		idemKey = &k
	}

	op, existed, err := h.repo.CreateOperation(r.Context(), tid, req.OpType, req.Input, idemKey, 3)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create operation", reqID(r))
		return
	}

	if existed {
		// Idempotent replay: same key seen before. Return 200 (not 201) with
		// the original operation so the client can tell it wasn't re-created.
		httpx.WriteJSON(w, http.StatusOK, op)
		return
	}

	job := queue.Job{
		OperationID: op.ID,
		TenantID:    tid,
		OpType:      op.OpType,
		Attempt:     0,
		EnqueuedAt:  time.Now(),
	}
	if err := h.q.Enqueue(r.Context(), job); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to enqueue operation", reqID(r))
		return
	}
	if err := h.repo.TransitionToQueued(r.Context(), op.ID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update operation state", reqID(r))
		return
	}
	op.Status = model.StatusQueued

	httpx.WriteJSON(w, http.StatusAccepted, op)
}

func (h *Handler) getOperation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	op, err := h.repo.GetOperation(r.Context(), tenantID(r), id)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "operation not found", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, op)
}

func (h *Handler) listOperations(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	ops, err := h.repo.ListOperations(r.Context(), repository.ListFilter{
		TenantID: tenantID(r),
		Status:   r.URL.Query().Get("status"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list operations", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"operations": ops, "limit": limit, "offset": offset})
}

func (h *Handler) cancelOperation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.repo.CancelOperation(r.Context(), tenantID(r), id); err != nil {
		httpx.WriteError(w, http.StatusConflict, "cannot_cancel", err.Error(), reqID(r))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type startAttemptReq struct {
	WorkerID      string `json:"worker_id"`
	AttemptNumber int    `json:"attempt_number"`
}

func (h *Handler) startAttempt(w http.ResponseWriter, r *http.Request) {
	opID := chi.URLParam(r, "id")
	var req startAttemptReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error(), reqID(r))
		return
	}
	attemptID, err := h.repo.StartAttempt(r.Context(), opID, req.WorkerID, req.AttemptNumber)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to start attempt", reqID(r))
		return
	}

	// Mark this tenant as occupying one more concurrency slot. Best-effort:
	// a Redis hiccup here must not fail the attempt itself, since the
	// concurrency limit is an admission-time throttle, not a correctness
	// guarantee — worst case, one over-limit operation slips through
	// briefly, which is a far smaller problem than losing the operation.
	if op, opErr := h.repo.GetOperationAnyTenant(r.Context(), opID); opErr == nil {
		if err := h.q.IncrConcurrency(r.Context(), op.TenantID); err != nil {
			log.Printf("operations: failed to increment concurrency counter for tenant %s: %v", op.TenantID, err)
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]string{"attempt_id": attemptID})
}

type completeAttemptReq struct {
	AttemptID     string          `json:"attempt_id"`
	Success       bool            `json:"success"`
	Output        json.RawMessage `json:"output,omitempty"`
	Provider      string          `json:"provider,omitempty"`
	LatencyMs     int             `json:"latency_ms,omitempty"`
	ErrorCategory string          `json:"error_category,omitempty"` // transient | permanent
	ErrorDetail   string          `json:"error_detail,omitempty"`
}

// completeAttempt is the crux of retry logic: it decides, based on error
// category and remaining attempts, whether to schedule a retry, or move
// the operation to a terminal failure state — and it is the only place
// that makes that decision, so the rule lives in one spot.
func (h *Handler) completeAttempt(w http.ResponseWriter, r *http.Request) {
	opID := chi.URLParam(r, "id")
	var req completeAttemptReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error(), reqID(r))
		return
	}

	op, err := h.repo.GetOperationAnyTenant(r.Context(), opID)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "operation not found", reqID(r))
		return
	}

	// This attempt is finishing (success, permanent failure, or about to be
	// rescheduled for retry) — either way it is no longer occupying a
	// concurrency slot right now.
	if err := h.q.DecrConcurrency(r.Context(), op.TenantID); err != nil {
		log.Printf("operations: failed to decrement concurrency counter for tenant %s: %v", op.TenantID, err)
	}

	if req.Success {
		if err := h.repo.CompleteAttemptSuccess(r.Context(), opID, req.AttemptID, req.Output, req.Provider, req.LatencyMs); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to record success", reqID(r))
			return
		}
		go h.notify.NotifySuccess(context.Background(), op.TenantID, op.ID, req.Output)
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": model.StatusSucceeded})
		return
	}

	willRetry := req.ErrorCategory == "transient" && op.AttemptCount < op.MaxRetries
	nextStatus, err := h.repo.CompleteAttemptFailure(r.Context(), opID, req.AttemptID, req.ErrorCategory, req.ErrorDetail, willRetry)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to record failure", reqID(r))
		return
	}

	if willRetry {
		job := queue.Job{
			OperationID: op.ID,
			TenantID:    op.TenantID,
			OpType:      op.OpType,
			Attempt:     op.AttemptCount,
			EnqueuedAt:  time.Now(),
		}
		delay := queue.BackoffDelay(op.AttemptCount)
		if err := h.q.ScheduleRetry(r.Context(), job, delay); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to schedule retry", reqID(r))
			return
		}
	} else if nextStatus == model.StatusFailed || nextStatus == model.StatusDeadLettered {
		go h.notify.NotifyTerminalFailure(context.Background(), op.TenantID, op.ID, nextStatus)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": nextStatus})
}

func (h *Handler) adminGetOperation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	op, err := h.repo.GetOperationAnyTenant(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "operation not found", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, op)
}

func (h *Handler) listAttempts(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	attempts, err := h.repo.ListAttempts(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list attempts", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"attempts": attempts})
}

func (h *Handler) adminListOperations(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	ops, err := h.repo.ListOperationsAnyTenant(r.Context(), repository.AdminListFilter{
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list operations", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"operations": ops})
}

func (h *Handler) adminSummary(w http.ResponseWriter, r *http.Request) {
	counts, err := h.repo.PlatformSummary(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load summary", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status_counts": counts})
}
