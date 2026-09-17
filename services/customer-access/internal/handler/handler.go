package handler

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/nexora/customer-access/internal/crypto"
	"github.com/nexora/customer-access/internal/httpx"
	"github.com/nexora/customer-access/internal/repository"
)

type Handler struct {
	repo         *repository.Repository
	hmacSecret   string
}

func New(repo *repository.Repository) *Handler {
	return &Handler{
		repo:       repo,
		hmacSecret: os.Getenv("API_KEY_HMAC_SECRET"),
	}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/internal/tenants", h.createTenant)
	r.Get("/internal/tenants", h.listTenants)
	r.Get("/internal/tenants/{tenantID}", h.getTenant)
	r.Post("/internal/tenants/{tenantID}/api-keys", h.createAPIKey)
	r.Get("/internal/tenants/{tenantID}/api-keys", h.listAPIKeys)
	r.Delete("/internal/api-keys/{keyID}", h.revokeAPIKey)
	r.Post("/internal/auth/validate", h.validateKey)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}

type createTenantReq struct {
	Name             string `json:"name"`
	Tier             string `json:"tier"`
	RateLimitRPM     int    `json:"rate_limit_rpm"`
	MonthlyQuota     int64  `json:"monthly_quota"`
	MaxConcurrentOps int    `json:"max_concurrent_ops"`
}

func (h *Handler) createTenant(w http.ResponseWriter, r *http.Request) {
	var req createTenantReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error(), reqID(r))
		return
	}
	if req.Name == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "name is required", reqID(r))
		return
	}
	if req.Tier == "" {
		req.Tier = "standard"
	}
	if req.RateLimitRPM == 0 {
		req.RateLimitRPM = 100
	}
	if req.MonthlyQuota == 0 {
		req.MonthlyQuota = 100000
	}
	if req.MaxConcurrentOps == 0 {
		req.MaxConcurrentOps = 10
	}

	tenant, err := h.repo.CreateTenant(r.Context(), req.Name, req.Tier, req.RateLimitRPM, req.MonthlyQuota, req.MaxConcurrentOps)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create tenant", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, tenant)
}

func (h *Handler) getTenant(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "tenantID")
	tenant, err := h.repo.GetTenant(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "tenant not found", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, tenant)
}

func (h *Handler) listTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.repo.ListTenants(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list tenants", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tenants": tenants})
}

// createAPIKey returns the raw key exactly once. It is never retrievable
// again — only the repository's hash and a display prefix persist.
func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	raw, prefix, err := crypto.GenerateRawKey()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to generate key", reqID(r))
		return
	}
	hash := crypto.HashKey(h.hmacSecret, raw)

	key, err := h.repo.CreateAPIKey(r.Context(), tenantID, prefix, hash)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create api key", reqID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id":         key.ID,
		"tenant_id":  key.TenantID,
		"key_prefix": key.KeyPrefix,
		"status":     key.Status,
		"created_at": key.CreatedAt,
		"raw_key":    raw, // shown once; dashboard must warn the user to save it now
	})
}

func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keys, err := h.repo.ListAPIKeys(r.Context(), tenantID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list keys", reqID(r))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"api_keys": keys})
}

func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "keyID")
	if err := h.repo.RevokeAPIKey(r.Context(), keyID); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error(), reqID(r))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type validateKeyReq struct {
	APIKey string `json:"api_key"`
}

// validateKey is called by the Gateway on (or near) every public request.
// It hashes the presented raw key the same way it was hashed at creation
// and looks up the still-active key + tenant limits in one query.
func (h *Handler) validateKey(w http.ResponseWriter, r *http.Request) {
	var req validateKeyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "api_key is required", reqID(r))
		return
	}

	hash := crypto.HashKey(h.hmacSecret, req.APIKey)
	v, err := h.repo.ValidateKeyHash(r.Context(), hash)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid_api_key", "key is invalid, revoked, or unknown", reqID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"tenant_id":           v.TenantID,
		"tier":                v.Tier,
		"rate_limit_rpm":      v.RateLimitRPM,
		"monthly_quota":       v.MonthlyQuota,
		"max_concurrent_ops":  v.MaxConcurrentOps,
	})
}

func reqID(r *http.Request) string {
	return r.Header.Get("X-Request-ID")
}
