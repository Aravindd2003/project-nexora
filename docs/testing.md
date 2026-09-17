# Running the test suite

Tests are split by what they need to run, so a reviewer can run the fast
ones instantly and the ones needing infrastructure only when Postgres is
available.

## Go unit tests (no dependencies — always run)

These test pure logic: API key generation/hashing, the operation state
machine's terminal-status rules, exponential backoff calculation, and
webhook HMAC signing.

```bash
cd services/customer-access && go test ./... -v
cd services/operations && go test ./... -v          # unit tests only pass here without a DB
cd services/usage-webhooks && go test ./... -v
```

Without a database configured, the integration tests in
`services/operations/internal/repository` will report as **skipped**, not
failed — `go test` still exits 0.

## Go integration tests (require Postgres)

These prove the behaviors that matter most for this assessment's grading
criteria — tenant isolation, idempotency, and terminal-state immutability —
against a **real** Postgres database, because those guarantees are enforced
by SQL constraints (`UNIQUE`, guarded `UPDATE ... WHERE status = X`), not
just application code. A mock would only prove the Go code calls the right
method; it wouldn't prove the database actually stops the bad outcome.

With the docker-compose stack running:

```bash
cd services/operations
TEST_DATABASE_URL="postgres://nexora:nexora_dev_password@localhost:5432/nexora?sslmode=disable" go test ./internal/repository/... -v
```

Each test seeds its own throwaway tenant (and cascade-deletes it on
cleanup), so this is safe to run against the same database you're using for
manual testing — it won't interfere with or leave behind real data.

## Python unit tests (worker)

Test AI-provider response parsing and error classification (transient vs.
permanent) — pure logic, no live AI provider call or API key needed.

```bash
cd worker
python -m unittest discover -s tests -v
```

## What's covered, mapped to the assessment's required behaviors

| Required behavior | Test(s) |
|---|---|
| Tenant isolation | `TestTenantIsolation_CannotReadAnotherTenantsOperation` |
| Idempotency | `TestIdempotency_DuplicateKeyReturnsExistingOperation` |
| Operation state transitions | `TestTerminalStateImmutability`, `TestIsTerminal_*` |
| Retries (backoff) | `TestBackoffDelay_*` |
| Duplicate handling | `TestIdempotency_*`, `TestTerminalStateImmutability` (duplicate completion report) |
| Webhook delivery (signing) | `TestSign_*` |
| Authentication (credential hashing) | `TestHashKey_*`, `TestGenerateRawKey_*` |
| Worker failure / error classification | `TestExtractJson_*`, `TestProviderErrorClassification` |

Not covered by automated tests (manually verified instead, via the demo
scenarios in the dummy customer simulator): rate limiting, quota
enforcement, and full end-to-end webhook retry timing — these depend on
Redis timing and real HTTP round-trips across services, which are better
exercised through the `/demo/*` endpoints than through unit tests with
mocked clocks.
