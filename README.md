# Nexora

A simplified but realistic implementation of **Nexora**, a B2B AI & automation
infrastructure platform, built for the Cipherion engineering assessment.

Customers submit asynchronous operations (classify, summarize, custom AI
tasks) via a REST API. Nexora authenticates the request, enqueues the work,
processes it on a background worker pool backed by an AI provider (with
automatic fallback), tracks the full lifecycle through a strict state
machine, meters usage, and notifies the customer via signed webhooks —
all while keeping every tenant's data completely isolated from every other.

## Quick start

```bash
cp .env.example .env
# edit .env and set GEMINI_API_KEY (free tier: https://aistudio.google.com/apikey)

docker-compose up --build
```

Once everything is healthy (roughly 30–60s for the Go services to compile
their images the first time):

```bash
./scripts/seed.sh
```

This creates a demo tenant and prints an API key. Then:

- **Customer Dashboard**: http://localhost:3000 — paste the API key on the
  API Keys page.
- **Dummy customer simulator**: http://localhost:4000 — configure it with
  the same key, then trigger `/demo/normal`, `/demo/burst`,
  `/demo/idempotent`, `/demo/webhook/toggle`, `/demo/quota-exhaust`.
- **Gateway API**: http://localhost:8080 (see API reference below).

## Architecture

```
Client (Dashboard / Dummy Customer)
        │  HTTP/REST + API key
        ▼
   API Gateway  ── auth, rate limiting, quota check, idempotency pass-through
        │
        ├──► Service 1: Customer & Access  ── tenants, API keys (HMAC-hashed)
        ├──► Service 2: Operations & Processing ── state machine, enqueue/retry
        └──► Service 3: Usage & Webhooks   ── usage ledger, signed webhook delivery
                     │
                     ▼
              Redis (queue + delayed retries)
                     │
                     ▼
              AI Processing Worker(s)  ── Gemini primary, OpenRouter fallback
                     │
                     ▼
              PostgreSQL (source of truth for everything above)
```

Each backend service is self-contained (own `go.mod`, own Dockerfile) and
communicates over internal HTTP. See `db/migrations/001_init.sql` for the
full relational schema across four domains: Tenant & Access, Operations,
Usage, and Integrations.

### Operation lifecycle

```
PENDING → QUEUED → RUNNING → SUCCEEDED
                       │ ↘
                       │  RETRYING → QUEUED (after backoff, if attempts remain)
                       │
                       └─→ FAILED (permanent error) or DEAD_LETTERED (retries exhausted)

Any non-terminal state → CANCELLED (client-initiated)
```

Once an operation reaches `SUCCEEDED`, `FAILED`, `DEAD_LETTERED`, or
`CANCELLED`, it is immutable — enforced at the SQL layer (`UPDATE ... WHERE
status = 'RUNNING'` guards) as well as in application logic.

## Key engineering decisions

- **Tenant isolation is enforced at the query layer, not filtered after
  the fact.** Every customer-facing repository method takes `tenantID` as
  a required parameter and includes it in the `WHERE` clause of the SQL
  itself (see `GetOperation` vs. the admin-only `GetOperationAnyTenant` in
  Service 2). The Gateway is also the only thing allowed to set
  `X-Tenant-ID` on downstream requests — it always overwrites whatever a
  client sent, so a forged header can never impersonate another tenant.

- **API keys are HMAC-SHA256 hashed, not bcrypt/argon2id.** Unlike
  passwords, API keys are already high-entropy random tokens — there's no
  weak-secret dictionary attack to slow down, so a fast keyed HMAC (which
  only Nexora, holding the secret, can compute) is the right tool. See
  `services/customer-access/internal/crypto/apikey.go`.

- **Retries use a Redis sorted-set delay queue, not sleeping workers.** A
  failed job is scheduled onto `nexora:queue:delayed` with a ready-at
  score; Service 2 runs a 1-second ticker that promotes due jobs into the
  main queue. This keeps workers stateless and always available for new
  work instead of blocking on `time.Sleep`.

- **Idempotency is enforced by a database constraint, not just an
  application check.** `UNIQUE(tenant_id, idempotency_key)` in Postgres is
  the actual guarantee; the repository layer handles the resulting
  `23505` race (two concurrent identical requests) by re-fetching the
  winning row rather than erroring.

- **Terminal-state side effects (usage metering, webhook dispatch) are
  triggered by Service 2**, which owns the state machine, rather than by
  the Worker. The worker only reports attempt outcomes; deciding "this
  operation just became SUCCEEDED, so let's bill for it and notify the
  customer" is state-machine logic and belongs with the state machine.

- **AI provider fallback does not retry a permanent error against the
  secondary provider.** If Gemini returns malformed output, that's a
  prompt/input problem, not a Gemini-availability problem — switching
  providers wouldn't fix it, so only `transient` failures (timeouts,
  5xx, 429) trigger the OpenRouter fallback.

## What's implemented (Tier 1 — Core)

- ✅ API Gateway: API-key auth, per-tenant rate limiting, monthly quota
  enforcement, per-tenant concurrency limiting, idempotency pass-through
- ✅ Service 1: tenant + API key lifecycle, hashed credential storage
- ✅ Service 2: full operation state machine, attempt tracking, exponential
  backoff retries
- ✅ Service 3: idempotent usage ledger, HMAC-signed webhook delivery with
  retry
- ✅ Worker: Gemini (primary) → OpenRouter (fallback) AI processing
- ✅ Customer Dashboard: overview, operations list/detail/cancel, API key
  management, webhook management
- ✅ PostgreSQL schema with tenant-scoped queries throughout
- ✅ Single `docker-compose up` startup
- ✅ Dummy customer simulator covering all required demo scenarios
- ✅ Automated tests covering tenant isolation, idempotency, terminal-state
  immutability, retry backoff, and credential/webhook signing — see
  [docs/testing.md](docs/testing.md) for how to run them and what each one
  proves

## Partially implemented (Tier 2)

- ✅ Idempotency-Key header support
- ✅ Webhook retry with backoff + HMAC-SHA256 signing
- ✅ AI provider fallback
- ✅ Quota enforcement
- ✅ Concurrency control — per-tenant occupancy tracked via a Redis counter
  that Service 2 increments/decrements as operations enter/leave RUNNING
- ✅ Correlation IDs (`X-Request-ID`) propagated through Gateway → services
- ✅ Dummy Customer Server with all demo endpoints
- ✅ Real-time-ish dashboard updates: both the operations list and the
  operation detail page poll every 3–5s (SSE/WebSocket were considered but
  polling was the pragmatic choice given the time budget — see submission
  guidelines section 5, "choosing simpler alternatives" is explicitly not
  penalized)
- ✅ Admin Dashboard: platform-wide summary (operation counts by status,
  tenant count) and a failed/dead-lettered operations view with click-to-
  expand attempt history, tracing Customer → Operation → Attempt → Failure
  Detail as specified. **Architecture note**: this is implemented as a
  `/admin` route within the same Next.js app as the Customer Dashboard
  (see `dashboard-customer/app/admin/page.tsx`), rather than as a fully
  separate deployable frontend. This was a deliberate time-boxing decision
  — it satisfies the functional requirement (a platform-wide, non-tenant-
  scoped operator view) without the added Docker/compose overhead of a
  second Next.js service. It is intentionally excluded from the customer
  app's navigation (no sidebar link) since a real operator would reach it
  via a separate internal URL, not by browsing the customer app.
- ✅ Dead-letter queue: operations reaching `DEAD_LETTERED` are visible via
  the Admin Dashboard above, with the same trace-back the spec asks for

## Not implemented (Tier 3 — bonus, out of scope for 1–2yr level)

Enterprise Worker / reverse tunnel, embedded AI assistant, cron scheduling,
gRPC internal communication, comprehensive automated test suite.

## Repository layout

```
gateway/                  API Gateway (Node/TypeScript, Express)
services/
  customer-access/        Service 1 (Go)
  operations/              Service 2 (Go)
  usage-webhooks/          Service 3 (Go)
worker/                    AI Processing Worker (Python)
dashboard-customer/        Customer Dashboard (Next.js)
dummy-customer/             Dummy customer simulator (Node)
db/migrations/              PostgreSQL schema
scripts/seed.sh              Bootstrap script for first tenant/key
docker-compose.yml
.env.example
```

## API reference (via Gateway, `http://localhost:8080`)

All endpoints except `/healthz` require `Authorization: Bearer <api_key>`.

| Method | Path | Description |
|---|---|---|
| POST | `/v1/operations` | Create an operation. Accepts optional `Idempotency-Key` header. |
| GET | `/v1/operations?status=&limit=&offset=` | List operations for the authenticated tenant. |
| GET | `/v1/operations/{id}` | Get operation detail. |
| POST | `/v1/operations/{id}/cancel` | Cancel a non-terminal operation. |
| GET | `/v1/usage` | Current month's usage summary and quota remaining. |
| POST | `/v1/webhooks` | Register a webhook endpoint. Returns signing secret once. |
| GET | `/v1/webhooks` | List registered webhook endpoints. |
| POST | `/v1/api-keys` | Create a new API key. Returns raw key once. |
| GET | `/v1/api-keys` | List API keys (prefix only, no secrets). |
| DELETE | `/v1/api-keys/{id}` | Revoke a key. |

Errors follow RFC 7807 Problem Details:
```json
{ "type": "about:blank", "title": "rate_limited", "status": 429, "detail": "...", "request_id": "..." }
```
