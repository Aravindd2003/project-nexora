package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nexora/customer-access/internal/model"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateTenant(ctx context.Context, name, tier string, rateLimit int, quota int64, maxConcurrent int) (*model.Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO tenants (name, tier, rate_limit_rpm, monthly_quota, max_concurrent_ops)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, tier, rate_limit_rpm, monthly_quota, max_concurrent_ops, created_at`,
		name, tier, rateLimit, quota, maxConcurrent)

	var t model.Tenant
	if err := row.Scan(&t.ID, &t.Name, &t.Tier, &t.RateLimitRPM, &t.MonthlyQuota, &t.MaxConcurrentOps, &t.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	return &t, nil
}

func (r *Repository) GetTenant(ctx context.Context, id string) (*model.Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, tier, rate_limit_rpm, monthly_quota, max_concurrent_ops, created_at
		FROM tenants WHERE id = $1`, id)

	var t model.Tenant
	if err := row.Scan(&t.ID, &t.Name, &t.Tier, &t.RateLimitRPM, &t.MonthlyQuota, &t.MaxConcurrentOps, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repository) ListTenants(ctx context.Context) ([]model.Tenant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, tier, rate_limit_rpm, monthly_quota, max_concurrent_ops, created_at
		FROM tenants ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Tenant
	for rows.Next() {
		var t model.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Tier, &t.RateLimitRPM, &t.MonthlyQuota, &t.MaxConcurrentOps, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) CreateAPIKey(ctx context.Context, tenantID, prefix, hash string) (*model.APIKey, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO api_keys (tenant_id, key_prefix, key_hash)
		VALUES ($1, $2, $3)
		RETURNING id, tenant_id, key_prefix, status, created_at`,
		tenantID, prefix, hash)

	var k model.APIKey
	if err := row.Scan(&k.ID, &k.TenantID, &k.KeyPrefix, &k.Status, &k.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}
	return &k, nil
}

func (r *Repository) ListAPIKeys(ctx context.Context, tenantID string) ([]model.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, key_prefix, status, created_at, revoked_at
		FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.APIKey
	for rows.Next() {
		var k model.APIKey
		if err := rows.Scan(&k.ID, &k.TenantID, &k.KeyPrefix, &k.Status, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *Repository) RevokeAPIKey(ctx context.Context, keyID string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE api_keys SET status = 'revoked', revoked_at = now()
		WHERE id = $1 AND status = 'active'`, keyID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("key not found or already revoked")
	}
	return nil
}

// ValidateKeyHash is the core of gateway authentication: given the HMAC
// hash of a presented key, find the still-active key and its tenant's
// current limits in one round trip. Returning limits here (not just the
// tenant ID) lets the Gateway make rate-limit/quota decisions without a
// second call.
type ValidatedKey struct {
	TenantID         string
	Tier             string
	RateLimitRPM     int
	MonthlyQuota     int64
	MaxConcurrentOps int
}

func (r *Repository) ValidateKeyHash(ctx context.Context, hash string) (*ValidatedKey, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT t.id, t.tier, t.rate_limit_rpm, t.monthly_quota, t.max_concurrent_ops
		FROM api_keys k
		JOIN tenants t ON t.id = k.tenant_id
		WHERE k.key_hash = $1 AND k.status = 'active'`, hash)

	var v ValidatedKey
	if err := row.Scan(&v.TenantID, &v.Tier, &v.RateLimitRPM, &v.MonthlyQuota, &v.MaxConcurrentOps); err != nil {
		return nil, err
	}
	return &v, nil
}
