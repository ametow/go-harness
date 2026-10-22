package main

import (
	"context"
	"sync"
)

// turnControl decides what Ctrl-C means. While a turn is running, the first
// Ctrl-C cancels just that turn (the model call or the running tool) and the
// chat carries on. At the prompt, or on a second Ctrl-C while the turn is
// still stopping, it means quit.
type turnControl struct {
	mu        sync.Mutex
	cancel    context.CancelFunc // set only while a turn is running
	cancelled bool               // this turn has already been cancelled once
}

// begin starts a turn and returns the context to run it under.
func (t *turnControl) begin() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel, t.cancelled = cancel, false
	t.mu.Unlock()
	return ctx
}

// end finishes the turn and releases its context.
func (t *turnControl) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
}

// interrupt handles one Ctrl-C and reports whether the program should exit.
func (t *turnControl) interrupt() (exit bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel == nil || t.cancelled {
		return true
	}
	t.cancelled = true
	t.cancel()
	return false
}
