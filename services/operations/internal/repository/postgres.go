package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nexora/operations/internal/model"
)

var ErrDuplicateIdempotencyKey = errors.New("operation with this idempotency key already exists")
var ErrNotFound = errors.New("operation not found")

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// CreateOperation inserts a new PENDING operation. If an idempotency key is
// supplied and a matching (tenant, key) pair already exists, the existing
// operation is returned instead — this is what makes create-operation safe
// to retry after a client-side timeout.
func (r *Repository) CreateOperation(ctx context.Context, tenantID, opType string, input json.RawMessage, idempotencyKey *string, maxRetries int) (*model.Operation, bool, error) {
	if idempotencyKey != nil {
		existing, err := r.getByIdempotencyKey(ctx, tenantID, *idempotencyKey)
		if err == nil {
			return existing, true, nil // found existing — not an error, just "already exists"
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, false, err
		}
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO operations (tenant_id, idempotency_key, op_type, status, input, max_retries)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, idempotency_key, op_type, status, input, output, error_message,
		          attempt_count, max_retries, created_at, started_at, completed_at`,
		tenantID, idempotencyKey, opType, model.StatusPending, input, maxRetries)

	op, err := scanOperation(row)
	if err != nil {
		// Two concurrent requests with the same idempotency key can both
		// pass the SELECT-miss above before either commits their INSERT
		// (classic check-then-act race). The unique index on
		// (tenant_id, idempotency_key) guarantees only one INSERT wins;
		// the loser lands here and should behave exactly like the
		// "found existing" path, not surface an error to the client.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && idempotencyKey != nil {
			existing, existErr := r.getByIdempotencyKey(ctx, tenantID, *idempotencyKey)
			if existErr == nil {
				return existing, true, nil
			}
		}
		return nil, false, fmt.Errorf("insert operation: %w", err)
	}
	return op, false, nil
}

func (r *Repository) getByIdempotencyKey(ctx context.Context, tenantID, key string) (*model.Operation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, idempotency_key, op_type, status, input, output, error_message,
		       attempt_count, max_retries, created_at, started_at, completed_at
		FROM operations WHERE tenant_id = $1 AND idempotency_key = $2`, tenantID, key)
	return scanOperation(row)
}

func (r *Repository) GetOperation(ctx context.Context, tenantID, id string) (*model.Operation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, idempotency_key, op_type, status, input, output, error_message,
		       attempt_count, max_retries, created_at, started_at, completed_at
		FROM operations WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	op, err := scanOperation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return op, err
}

// GetOperationAnyTenant is used ONLY by the admin dashboard path (Service 2
// internal API), which is allowed to cross tenant boundaries. All
// customer-facing queries must go through GetOperation, which is
// tenant-scoped in the WHERE clause itself — not filtered after the fact.
func (r *Repository) GetOperationAnyTenant(ctx context.Context, id string) (*model.Operation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, idempotency_key, op_type, status, input, output, error_message,
		       attempt_count, max_retries, created_at, started_at, completed_at
		FROM operations WHERE id = $1`, id)
	op, err := scanOperation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return op, err
}

type ListFilter struct {
	TenantID string
	Status   string // optional
	Limit    int
	Offset   int
}

func (r *Repository) ListOperations(ctx context.Context, f ListFilter) ([]model.Operation, error) {
	query := `
		SELECT id, tenant_id, idempotency_key, op_type, status, input, output, error_message,
		       attempt_count, max_retries, created_at, started_at, completed_at
		FROM operations WHERE tenant_id = $1`
	args := []any{f.TenantID}

	if f.Status != "" {
		args = append(args, f.Status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	query += " ORDER BY created_at DESC"
	args = append(args, f.Limit)
	query += fmt.Sprintf(" LIMIT $%d", len(args))
	args = append(args, f.Offset)
	query += fmt.Sprintf(" OFFSET $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Operation
	for rows.Next() {
		op, err := scanOperationRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *op)
	}
	return out, rows.Err()
}

// AdminListFilter is deliberately a separate type from ListFilter (which
// requires a TenantID) — this makes the cross-tenant nature of the admin
// query visible at the call site rather than something achieved by passing
// an empty tenant ID into the customer-facing path.
type AdminListFilter struct {
	Status string
	Limit  int
	Offset int
}

// ListOperationsAnyTenant powers the Admin Dashboard's failed/dead-lettered
// operations view. It intentionally has no tenant_id in its WHERE clause —
// this is the one place in the codebase that is allowed to see across
// tenants, and it is never reachable from the customer-facing API surface.
func (r *Repository) ListOperationsAnyTenant(ctx context.Context, f AdminListFilter) ([]model.Operation, error) {
	query := `
		SELECT id, tenant_id, idempotency_key, op_type, status, input, output, error_message,
		       attempt_count, max_retries, created_at, started_at, completed_at
		FROM operations`
	var args []any

	if f.Status != "" {
		args = append(args, f.Status)
		query += fmt.Sprintf(" WHERE status = $%d", len(args))
	}
	query += " ORDER BY created_at DESC"
	args = append(args, f.Limit)
	query += fmt.Sprintf(" LIMIT $%d", len(args))
	args = append(args, f.Offset)
	query += fmt.Sprintf(" OFFSET $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Operation
	for rows.Next() {
		op, err := scanOperationRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *op)
	}
	return out, rows.Err()
}

// PlatformSummary gives the Admin Dashboard's headline numbers: how many
// operations sit in each status right now, across every tenant.
func (r *Repository) PlatformSummary(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `SELECT status, COUNT(*) FROM operations GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

// TransitionToQueued moves PENDING -> QUEUED. Called right after a
// successful enqueue so DB state and queue state never disagree about
// whether a job was actually submitted.
func (r *Repository) TransitionToQueued(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE operations SET status = $1 WHERE id = $2 AND status = $3`,
		model.StatusQueued, id, model.StatusPending)
	return err
}

// StartAttempt records a new attempt and moves the operation to RUNNING.
func (r *Repository) StartAttempt(ctx context.Context, operationID, workerID string, attemptNumber int) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE operations SET status = $1, attempt_count = $2,
		       started_at = COALESCE(started_at, now())
		WHERE id = $3`, model.StatusRunning, attemptNumber, operationID)
	if err != nil {
		return "", err
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO operation_attempts (operation_id, attempt_number, worker_id)
		VALUES ($1, $2, $3) RETURNING id`, operationID, attemptNumber, workerID)
	var attemptID string
	if err := row.Scan(&attemptID); err != nil {
		return "", err
	}
	return attemptID, tx.Commit(ctx)
}

// CompleteAttemptSuccess finalizes the attempt and the operation together.
func (r *Repository) CompleteAttemptSuccess(ctx context.Context, operationID, attemptID string, output json.RawMessage, provider string, latencyMs int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE operation_attempts SET finished_at = now(), success = true,
		       provider_used = $1, latency_ms = $2
		WHERE id = $3`, provider, latencyMs, attemptID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE operations SET status = $1, output = $2, completed_at = now(), error_message = NULL
		WHERE id = $3 AND status = $4`, // guard: only from RUNNING (terminal immutability)
		model.StatusSucceeded, output, operationID, model.StatusRunning)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CompleteAttemptFailure finalizes the attempt as failed and decides,
// atomically, whether the operation should retry, or move to a terminal
// failure state (FAILED for permanent errors, DEAD_LETTERED once retries
// are exhausted on a transient error).
func (r *Repository) CompleteAttemptFailure(ctx context.Context, operationID, attemptID, errCategory, errDetail string, willRetry bool) (nextStatus string, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE operation_attempts SET finished_at = now(), success = false,
		       error_category = $1, error_detail = $2
		WHERE id = $3`, errCategory, errDetail, attemptID)
	if err != nil {
		return "", err
	}

	if willRetry {
		nextStatus = model.StatusRetrying
	} else if errCategory == "permanent" {
		nextStatus = model.StatusFailed
	} else {
		nextStatus = model.StatusDeadLettered
	}

	_, err = tx.Exec(ctx, `
		UPDATE operations SET status = $1, error_message = $2,
		       completed_at = CASE WHEN $1 IN ('FAILED','DEAD_LETTERED') THEN now() ELSE completed_at END
		WHERE id = $3 AND status = $4`,
		nextStatus, errDetail, operationID, model.StatusRunning)
	if err != nil {
		return "", err
	}
	return nextStatus, tx.Commit(ctx)
}

func (r *Repository) CancelOperation(ctx context.Context, tenantID, id string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE operations SET status = $1, completed_at = now()
		WHERE id = $2 AND tenant_id = $3 AND status NOT IN ($4, $5, $6, $7)`,
		model.StatusCancelled, id, tenantID,
		model.StatusSucceeded, model.StatusFailed, model.StatusDeadLettered, model.StatusCancelled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("operation cannot be cancelled (not found or already terminal)")
	}
	return nil
}

func (r *Repository) ListAttempts(ctx context.Context, operationID string) ([]model.Attempt, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, operation_id, attempt_number, worker_id, provider_used,
		       started_at, finished_at, success, error_category, error_detail, latency_ms
		FROM operation_attempts WHERE operation_id = $1 ORDER BY attempt_number ASC`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Attempt
	for rows.Next() {
		var a model.Attempt
		if err := rows.Scan(&a.ID, &a.OperationID, &a.AttemptNumber, &a.WorkerID, &a.ProviderUsed,
			&a.StartedAt, &a.FinishedAt, &a.Success, &a.ErrorCategory, &a.ErrorDetail, &a.LatencyMs); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- scanning helpers (pgx.Row and pgx.Rows share no common interface for Scan args here,
// so we accept the minimal interface each needs) ---

type scanner interface {
	Scan(dest ...any) error
}

func scanOperation(row scanner) (*model.Operation, error) {
	var op model.Operation
	var output []byte
	if err := row.Scan(&op.ID, &op.TenantID, &op.IdempotencyKey, &op.OpType, &op.Status, &op.Input, &output,
		&op.ErrorMessage, &op.AttemptCount, &op.MaxRetries, &op.CreatedAt, &op.StartedAt, &op.CompletedAt); err != nil {
		return nil, err
	}
	if output != nil {
		op.Output = output
	}
	return &op, nil
}

func scanOperationRows(rows scanner) (*model.Operation, error) {
	return scanOperation(rows)
}

var _ = time.Now // keep time import if unused elsewhere in future edits
