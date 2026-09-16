-- Nexora core schema
-- Tenant & Access domain
CREATE TABLE tenants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    tier            TEXT NOT NULL DEFAULT 'standard',      -- standard | enterprise
    rate_limit_rpm  INT NOT NULL DEFAULT 100,               -- requests/minute
    monthly_quota   BIGINT NOT NULL DEFAULT 100000,         -- processing units/month
    max_concurrent_ops INT NOT NULL DEFAULT 10,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key_prefix      TEXT NOT NULL,             -- first 8 chars shown to user (e.g. nx_live_ab12)
    key_hash        TEXT NOT NULL,             -- HMAC-SHA256(secret, raw_key), never plaintext
    status          TEXT NOT NULL DEFAULT 'active', -- active | revoked
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ
);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);
CREATE UNIQUE INDEX idx_api_keys_hash ON api_keys(key_hash);

-- Operations domain
CREATE TABLE operations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    idempotency_key TEXT,
    op_type         TEXT NOT NULL,             -- classify | summarize | analyze | custom
    status          TEXT NOT NULL DEFAULT 'PENDING',
        -- PENDING | QUEUED | RUNNING | RETRYING | SUCCEEDED | FAILED | DEAD_LETTERED | CANCELLED
    input           JSONB NOT NULL,
    output          JSONB,
    error_message   TEXT,
    attempt_count   INT NOT NULL DEFAULT 0,
    max_retries     INT NOT NULL DEFAULT 3,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ
);
CREATE INDEX idx_operations_tenant ON operations(tenant_id, created_at DESC);
CREATE INDEX idx_operations_status ON operations(status);
-- idempotency key must be unique PER TENANT (not globally)
CREATE UNIQUE INDEX idx_operations_tenant_idem ON operations(tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE operation_attempts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id    UUID NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
    attempt_number  INT NOT NULL,
    worker_id       TEXT,
    provider_used   TEXT,                      -- gemini | openrouter | ...
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ,
    success         BOOLEAN,
    error_category  TEXT,                       -- transient | permanent | provider_error
    error_detail    TEXT,
    latency_ms      INT
);
CREATE INDEX idx_attempts_operation ON operation_attempts(operation_id);

-- Usage domain (append-only ledger)
CREATE TABLE usage_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    operation_id    UUID NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
    units           INT NOT NULL,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(operation_id)   -- guarantees idempotent metering even on retry/duplicate webhook
);
CREATE INDEX idx_usage_tenant_time ON usage_events(tenant_id, recorded_at);

-- Integrations domain
CREATE TABLE webhook_endpoints (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    url             TEXT NOT NULL,
    secret          TEXT NOT NULL,             -- for HMAC signing (bonus)
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_webhooks_tenant ON webhook_endpoints(tenant_id);

CREATE TABLE webhook_deliveries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id      UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    operation_id    UUID NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,             -- operation.succeeded | operation.failed | operation.dead_lettered
    status          TEXT NOT NULL DEFAULT 'PENDING', -- PENDING | DELIVERED | FAILED
    attempt_count   INT NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_deliveries_status ON webhook_deliveries(status, next_attempt_at);
