package main

import (
	"strings"
	"testing"
)

// msg makes a message of roughly `tokens` estimated tokens.
func msg(role string, tokens int) Message {
	return Message{Role: role, Content: strings.Repeat("x", tokens*4)}
}

func toolCallMsg(name string) Message {
	call := ToolCall{}
	call.Function.Name = name
	return Message{Role: "assistant", ToolCalls: []ToolCall{call}}
}

func TestTrimHistory(t *testing.T) {
	sys := Message{Role: "system", Content: "sys"}
	// Three turns; the middle one used a tool, so it has 4 messages.
	turn1 := []Message{msg("user", 100), msg("assistant", 100)}
	turn2 := []Message{msg("user", 100), toolCallMsg("read_file"), {Role: "tool", Content: "data"}, msg("assistant", 100)}
	turn3 := []Message{msg("user", 100), msg("assistant", 100)}
	build := func(turns ...[]Message) []Message {
		h := []Message{sys}
		for _, tn := range turns {
			h = append(h, tn...)
		}
		return h
	}

	tests := []struct {
		name        string
		history     []Message
		budget      int
		wantDropped int
		wantFirst   string // content prefix of the first kept non-system message
	}{
		{"under budget: unchanged", build(turn1, turn2, turn3), 10000, 0, ""},
		{"no limit when budget is 0", build(turn1, turn2, turn3), 0, 0, ""},
		{"drops oldest turn first", build(turn1, turn2, turn3), 450, 2, "x"},
		{"drops a whole tool turn, never splitting it", build(turn1, turn2, turn3), 250, 6, "x"},
		{"last turn kept even if over budget", build(turn1, turn2, turn3), 10, 6, "x"},
		{"single turn: nothing to drop", build(turn3), 10, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dropped := trimHistory(tt.history, tt.budget)
			if dropped != tt.wantDropped {
				t.Fatalf("dropped = %d, want %d", dropped, tt.wantDropped)
			}
			if len(got) != len(tt.history)-dropped {
				t.Fatalf("kept %d messages, want %d", len(got), len(tt.history)-dropped)
			}
			if got[0].Role != "system" {
				t.Fatal("system message must stay first")
			}
			if len(got) > 1 && got[1].Role != "user" {
				t.Fatalf("history must resume at a user message, got %q", got[1].Role)
			}
			if got[len(got)-1].Content != tt.history[len(tt.history)-1].Content {
				t.Fatal("latest message must be kept")
			}
		})
	}
}

func TestTrimHistoryDoesNotModifyInput(t *testing.T) {
	h := []Message{{Role: "system", Content: "s"}, msg("user", 100), msg("assistant", 100), msg("user", 100), msg("assistant", 100)}
	before := len(h)
	trimHistory(h, 150)
	if len(h) != before || h[1].Role != "user" || len(h[1].Content) != 400 {
		t.Fatal("input history was modified")
	}
}
