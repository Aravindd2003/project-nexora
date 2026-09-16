package model

import "time"

type Tenant struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Tier             string    `json:"tier"`
	RateLimitRPM     int       `json:"rate_limit_rpm"`
	MonthlyQuota     int64     `json:"monthly_quota"`
	MaxConcurrentOps int       `json:"max_concurrent_ops"`
	CreatedAt        time.Time `json:"created_at"`
}

type APIKey struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	KeyPrefix string     `json:"key_prefix"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}
