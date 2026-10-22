package main

import (
	"context"
	"errors"
	"testing"
)

func TestTurnControl(t *testing.T) {
	t.Run("Ctrl-C at the prompt quits", func(t *testing.T) {
		var tc turnControl
		if !tc.interrupt() {
			t.Fatal("no turn is running, so Ctrl-C should quit")
		}
	})

	t.Run("first Ctrl-C cancels the turn, second quits", func(t *testing.T) {
		var tc turnControl
		ctx := tc.begin()
		if tc.interrupt() {
			t.Fatal("the first Ctrl-C during a turn should cancel it, not quit")
		}
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("turn context err = %v, want Canceled", ctx.Err())
		}
		if !tc.interrupt() {
			t.Fatal("a second Ctrl-C while the turn is still stopping should quit")
		}
	})

	t.Run("after the turn ends, Ctrl-C quits again", func(t *testing.T) {
		var tc turnControl
		ctx := tc.begin()
		tc.end()
		if ctx.Err() == nil {
			t.Fatal("end should release the turn's context")
		}
		if !tc.interrupt() {
			t.Fatal("no turn is running any more, so Ctrl-C should quit")
		}
	})

	t.Run("each turn starts fresh", func(t *testing.T) {
		var tc turnControl
		tc.begin()
		tc.interrupt() // cancels turn one
		tc.end()
		ctx := tc.begin()
		if ctx.Err() != nil {
			t.Fatal("a new turn must not start cancelled")
		}
		if tc.interrupt() {
			t.Fatal("the first Ctrl-C of the new turn should cancel it, not quit")
		}
	})
}
