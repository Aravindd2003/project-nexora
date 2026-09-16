package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nexora/usage-webhooks/internal/dispatcher"
	"github.com/nexora/usage-webhooks/internal/handler"
	"github.com/nexora/usage-webhooks/internal/repository"
)

func main() {
	ctx := context.Background()

	pool, err := connectWithRetry(ctx, os.Getenv("DATABASE_URL"), 10, 2*time.Second)
	if err != nil {
		log.Fatalf("usage-webhooks: postgres connection failed: %v", err)
	}
	defer pool.Close()

	repo := repository.New(pool)
	disp := dispatcher.New(repo)
	h := handler.New(repo, disp)

	go retryLoop(ctx, disp)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}
	log.Printf("usage-webhooks: listening on :%s", port)
	if err := http.ListenAndServe(":"+port, h.Routes()); err != nil {
		log.Fatalf("usage-webhooks: server error: %v", err)
	}
}

func retryLoop(ctx context.Context, disp *dispatcher.Dispatcher) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if _, err := disp.RetryDue(ctx); err != nil {
			log.Printf("usage-webhooks: retry sweep error: %v", err)
		}
	}
}

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
		log.Printf("usage-webhooks: postgres not ready (attempt %d/%d): %v", i+1, attempts, lastErr)
		time.Sleep(delay)
	}
	return nil, lastErr
}
