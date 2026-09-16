package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nexora/operations/internal/handler"
	"github.com/nexora/operations/internal/queue"
	"github.com/nexora/operations/internal/repository"
	"github.com/nexora/operations/internal/sideeffects"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	pool, err := connectPGWithRetry(ctx, os.Getenv("DATABASE_URL"), 10, 2*time.Second)
	if err != nil {
		log.Fatalf("operations: postgres connection failed: %v", err)
	}
	defer pool.Close()

	opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		log.Fatalf("operations: invalid REDIS_URL: %v", err)
	}
	rdb := redis.NewClient(opt)
	if err := pingRedisWithRetry(ctx, rdb, 10, 2*time.Second); err != nil {
		log.Fatalf("operations: redis connection failed: %v", err)
	}

	repo := repository.New(pool)
	q := queue.New(rdb)
	notify := sideeffects.New(os.Getenv("USAGE_WEBHOOKS_URL"))
	h := handler.New(repo, q, notify)

	// Background scheduler: promotes delayed retries into the main queue
	// once their backoff window has elapsed. Runs independently of any
	// single HTTP request.
	go retryScheduler(ctx, q)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}
	log.Printf("operations: listening on :%s", port)
	if err := http.ListenAndServe(":"+port, h.Routes()); err != nil {
		log.Fatalf("operations: server error: %v", err)
	}
}

func retryScheduler(ctx context.Context, q *queue.Queue) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		promoted, err := q.PromoteDueRetries(ctx)
		if err != nil {
			log.Printf("operations: retry scheduler error: %v", err)
			continue
		}
		if promoted > 0 {
			log.Printf("operations: promoted %d delayed job(s) to main queue", promoted)
		}
	}
}

func connectPGWithRetry(ctx context.Context, url string, attempts int, delay time.Duration) (*pgxpool.Pool, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				return pool, nil
			} else {
				lastErr = pingErr
				pool.Close()
			}
		} else {
			lastErr = err
		}
		log.Printf("operations: postgres not ready (attempt %d/%d): %v", i+1, attempts, lastErr)
		time.Sleep(delay)
	}
	return nil, lastErr
}

func pingRedisWithRetry(ctx context.Context, rdb *redis.Client, attempts int, delay time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := rdb.Ping(ctx).Err(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		log.Printf("operations: redis not ready (attempt %d/%d): %v", i+1, attempts, lastErr)
		time.Sleep(delay)
	}
	return lastErr
}
