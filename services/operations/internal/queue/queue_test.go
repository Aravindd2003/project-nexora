package queue

import (
	"testing"
	"time"
)

func TestBackoffDelay_IncreasesWithAttemptNumber(t *testing.T) {
	d0 := BackoffDelay(0)
	d1 := BackoffDelay(1)
	d2 := BackoffDelay(2)

	// Jitter means we can't assert exact values, but the base delay must
	// strictly increase — a retry schedule that doesn't back off further
	// on repeated failures would hammer a struggling downstream dependency
	// instead of giving it room to recover.
	if d1 <= d0 {
		t.Errorf("expected backoff for attempt 1 (%v) to exceed attempt 0 (%v)", d1, d0)
	}
	if d2 <= d1 {
		t.Errorf("expected backoff for attempt 2 (%v) to exceed attempt 1 (%v)", d2, d1)
	}
}

func TestBackoffDelay_IsCapped(t *testing.T) {
	// At high attempt numbers, exponential growth must be capped rather
	// than growing unboundedly (which could otherwise delay a retry by
	// hours after just a handful of failures).
	d := BackoffDelay(20)
	const maxAllowed = 60*time.Second + 60*time.Second*20/100 // cap + max jitter

	if d > maxAllowed {
		t.Errorf("expected backoff to be capped near 60s (plus jitter), got %v", d)
	}
}

func TestBackoffDelay_NeverZeroOrNegative(t *testing.T) {
	for attempt := 0; attempt <= 10; attempt++ {
		d := BackoffDelay(attempt)
		if d <= 0 {
			t.Errorf("BackoffDelay(%d) returned non-positive duration: %v", attempt, d)
		}
	}
}
