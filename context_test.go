package main

import (
	"context"
	"errors"
	"fmt"
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

// fakeChat records the prompts it receives and replies with a numbered summary.
type fakeChat struct {
	prompts []string
	err     error
}

func (f *fakeChat) chat(_ context.Context, msgs []Message) (Message, error) {
	if f.err != nil {
		return Message{}, f.err
	}
	f.prompts = append(f.prompts, msgs[len(msgs)-1].Content)
	return Message{Role: "assistant", Content: fmt.Sprintf("SUMMARY-%d", len(f.prompts))}, nil
}

func longChat(turns int) []Message {
	h := []Message{{Role: "system", Content: "sys"}}
	for i := 1; i <= turns; i++ {
		h = append(h, Message{Role: "user", Content: fmt.Sprintf("question-%d %s", i, strings.Repeat("x", 800))})
		h = append(h, Message{Role: "assistant", Content: fmt.Sprintf("answer-%d", i)})
	}
	return h
}

func TestSummarizerView(t *testing.T) {
	const budget = 800 // summaryReserve leaves ~400 for history: about two of these turns

	t.Run("nothing dropped: no model call", func(t *testing.T) {
		f := &fakeChat{}
		s := &Summarizer{chat: f.chat}
		h := longChat(1)
		view, dropped, err := s.View(context.Background(), h, budget)
		if err != nil || dropped != 0 || len(view) != len(h) || len(f.prompts) != 0 {
			t.Fatalf("dropped=%d err=%v calls=%d", dropped, err, len(f.prompts))
		}
	})

	t.Run("summarizes dropped turns and caches the result", func(t *testing.T) {
		f := &fakeChat{}
		s := &Summarizer{chat: f.chat}
		h := longChat(5)

		view, dropped, err := s.View(context.Background(), h, budget)
		if err != nil || dropped == 0 {
			t.Fatalf("dropped=%d err=%v", dropped, err)
		}
		if len(f.prompts) != 1 || !strings.Contains(f.prompts[0], "question-1") {
			t.Fatalf("summarizer should have seen the oldest turn: %q", f.prompts)
		}
		if !strings.HasPrefix(view[0].Content, "sys") || !strings.Contains(view[0].Content, "SUMMARY-1") {
			t.Fatalf("system message = %q", view[0].Content)
		}
		if view[1].Role != "user" || view[len(view)-1].Content != "answer-5" {
			t.Fatal("view must resume at a user message and end with the latest reply")
		}
		if h[0].Content != "sys" {
			t.Fatal("history[0] was modified")
		}

		// Same history again: the cached summary is reused.
		if _, _, err := s.View(context.Background(), h, budget); err != nil || len(f.prompts) != 1 {
			t.Fatalf("expected cache hit, got %d calls (err %v)", len(f.prompts), err)
		}

		// A longer history drops more: only the newly dropped turns are summarized,
		// and the previous summary is passed along.
		h = append(h, longChat(7)[11:]...) // adds turns 6 and 7
		if _, _, err := s.View(context.Background(), h, budget); err != nil {
			t.Fatal(err)
		}
		if len(f.prompts) != 2 || !strings.Contains(f.prompts[1], "SUMMARY-1") {
			t.Fatalf("second call should include the previous summary: %q", f.prompts)
		}
	})

	t.Run("failure falls back to plain trimming", func(t *testing.T) {
		s := &Summarizer{chat: (&fakeChat{err: errors.New("model down")}).chat}
		view, dropped, err := s.View(context.Background(), longChat(5), budget)
		if err == nil || dropped == 0 {
			t.Fatalf("dropped=%d err=%v; want an error and a trimmed view", dropped, err)
		}
		if strings.Contains(view[0].Content, "Summary") || view[1].Role != "user" {
			t.Fatalf("fallback view looks wrong: %q", view[0].Content)
		}
	})

	t.Run("nil summarizer just trims", func(t *testing.T) {
		var s *Summarizer
		_, dropped, err := s.View(context.Background(), longChat(5), budget)
		if err != nil || dropped == 0 {
			t.Fatalf("dropped=%d err=%v", dropped, err)
		}
	})
}

func TestRenderTranscriptLimits(t *testing.T) {
	long := Message{Role: "tool", Content: strings.Repeat("y", 5000)}
	if got := renderTranscript([]Message{long}); len(got) > maxSummaryMsgChars+20 {
		t.Fatalf("single message not cut: %d chars", len(got))
	}
	var many []Message
	for i := 0; i < 100; i++ {
		many = append(many, Message{Role: "user", Content: strings.Repeat("z", 500)})
	}
	if got := renderTranscript(many); len(got) > maxSummaryInputChars+10 {
		t.Fatalf("transcript not capped: %d chars", len(got))
	}
}
