package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nexora/customer-access/internal/handler"
	"github.com/nexora/customer-access/internal/repository"
)

func main() {
	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	pool, err := connectWithRetry(ctx, dbURL, 10, 2*time.Second)
	if err != nil {
		log.Fatalf("customer-access: failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	repo := repository.New(pool)
	h := handler.New(repo)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	log.Printf("customer-access: listening on :%s", port)
	if err := http.ListenAndServe(":"+port, h.Routes()); err != nil {
		log.Fatalf("customer-access: server error: %v", err)
	}
}

// connectWithRetry tolerates Postgres not being ready yet when the
// container starts, which is common under docker-compose without
// depends_on health-gating every consumer perfectly.
func connectWithRetry(ctx context.Context, url string, attempts int, delay time.Duration) (*pgxpool.Pool, error) {
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
		log.Printf("customer-access: postgres not ready (attempt %d/%d): %v", i+1, attempts, lastErr)
		time.Sleep(delay)
	}
	return nil, lastErr
}
