package model

import "testing"

func TestIsTerminal_TerminalStatuses(t *testing.T) {
	terminal := []string{StatusSucceeded, StatusFailed, StatusDeadLettered, StatusCancelled}
	for _, status := range terminal {
		if !IsTerminal(status) {
			t.Errorf("expected %q to be terminal, but IsTerminal returned false", status)
		}
	}
}

func TestIsTerminal_NonTerminalStatuses(t *testing.T) {
	nonTerminal := []string{StatusPending, StatusQueued, StatusRunning, StatusRetrying}
	for _, status := range nonTerminal {
		if IsTerminal(status) {
			t.Errorf("expected %q to be non-terminal, but IsTerminal returned true", status)
		}
	}
}

func TestIsTerminal_UnknownStatusIsNotTerminal(t *testing.T) {
	// An unrecognized status string must default to "not terminal" (map
	// lookup miss returns the zero value, false) rather than panicking or
	// defaulting to true — the latter would let a typo silently freeze an
	// operation that should still be processable.
	if IsTerminal("SOME_MADE_UP_STATUS") {
		t.Error("unknown status should not be reported as terminal")
	}
}
