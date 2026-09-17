package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	mainQueueKey    = "nexora:queue:main"    // Redis List — workers BRPOP from here
	delayedQueueKey = "nexora:queue:delayed" // Redis ZSET — score = ready_at unix ts
)

// Job is the envelope every queued task carries. It intentionally holds
// only identifiers and metadata, not the operation payload itself — the
// worker fetches the full operation from Service 2 by ID. This keeps queue
// messages small and avoids two sources of truth for operation state.
type Job struct {
	OperationID string    `json:"operation_id"`
	TenantID    string    `json:"tenant_id"`
	OpType      string    `json:"op_type"`
	Attempt     int       `json:"attempt"`
	EnqueuedAt  time.Time `json:"enqueued_at"`
}

type Queue struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Queue {
	return &Queue{rdb: rdb}
}

// Enqueue pushes a job for immediate processing.
func (q *Queue) Enqueue(ctx context.Context, job Job) error {
	b, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	return q.rdb.LPush(ctx, mainQueueKey, b).Err()
}

// ScheduleRetry places a job on the delayed set, to become eligible for the
// main queue after `delay`. This implements exponential backoff without
// requiring workers to sleep (which would block them from other work).
func (q *Queue) ScheduleRetry(ctx context.Context, job Job, delay time.Duration) error {
	b, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	readyAt := float64(time.Now().Add(delay).Unix())
	return q.rdb.ZAdd(ctx, delayedQueueKey, redis.Z{Score: readyAt, Member: b}).Err()
}

// PromoteDueRetries moves any delayed jobs whose ready_at has passed into
// the main queue. Intended to be called on a short ticker (~1s) by Service
// 2's background scheduler. Returns the count promoted.
func (q *Queue) PromoteDueRetries(ctx context.Context) (int, error) {
	now := float64(time.Now().Unix())
	members, err := q.rdb.ZRangeByScore(ctx, delayedQueueKey, &redis.ZRangeBy{
		Min: "0", Max: fmt.Sprintf("%f", now),
	}).Result()
	if err != nil {
		return 0, err
	}
	if len(members) == 0 {
		return 0, nil
	}

	pipe := q.rdb.TxPipeline()
	for _, m := range members {
		pipe.LPush(ctx, mainQueueKey, m)
		pipe.ZRem(ctx, delayedQueueKey, m)
	}
	_, err = pipe.Exec(ctx)
	return len(members), err
}

// BackoffDelay computes exponential backoff with jitter: base * 2^attempt,
// capped, plus up to 20% random jitter to avoid thundering-herd retries.
func BackoffDelay(attempt int) time.Duration {
	base := 2 * time.Second
	max := 60 * time.Second
	d := base * time.Duration(1<<uint(attempt))
	if d > max {
		d = max
	}
	jitter := time.Duration(float64(d) * 0.2 * jitterFraction())
	return d + jitter
}

func jitterFraction() float64 {
	// Deterministic-enough pseudo-jitter without pulling in math/rand
	// wiring at every call site; good enough since this only spaces out
	// retries, it isn't security-sensitive.
	return float64(time.Now().UnixNano()%1000) / 1000.0
}
