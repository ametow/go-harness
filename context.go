package main

import (
	"fmt"
	"strings"
)

// A model can only read so much at once (its context window). Everything we
// send each call -- system prompt, every past message, every tool result --
// counts against it, so a long chat eventually overflows and the oldest
// parts get silently cut off or quality drops. trimHistory keeps the part we
// send under a budget by dropping the oldest conversation turns ourselves.

// estimateTokens is a rough size estimate: about 4 characters per token for
// English, plus a few tokens of per-message overhead. It doesn't need to be
// exact, only close enough to stay under the real limit.
func estimateTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		chars := len(m.Role) + len(m.Content)
		for _, tc := range m.ToolCalls {
			chars += len(tc.Function.Name) + len(fmt.Sprint(tc.Function.Arguments))
		}
		total += chars/4 + 4
	}
	return total
}

// trimHistory returns the messages to actually send, and how many it dropped.
// It never modifies history itself, so the full conversation stays intact for
// saving.
//
// Rules:
//   - history[0], the system message, is always kept.
//   - Messages are dropped oldest-first, one whole turn at a time. A turn
//     starts at a user message and includes the assistant replies and tool
//     results that followed. Cutting only at turn boundaries means a tool call
//     is never separated from its result, which models handle badly.
//   - The latest turn is always kept, even if it alone is over budget: it is
//     the question being answered right now.
//   - budget <= 0 means no limit.
func trimHistory(history []Message, budget int) ([]Message, int) {
	if budget <= 0 || estimateTokens(history) <= budget {
		return history, 0
	}

	// Indexes where each turn starts.
	var starts []int
	for i := 1; i < len(history); i++ {
		if history[i].Role == "user" {
			starts = append(starts, i)
		}
	}

	// Drop turns from the front until it fits, always leaving the last one.
	for drop := 1; drop < len(starts); drop++ {
		kept := append([]Message{history[0]}, history[starts[drop]:]...)
		if estimateTokens(kept) <= budget {
			return kept, starts[drop] - 1
		}
	}
	if len(starts) <= 1 {
		return history, 0 // only one turn: nothing we can drop
	}
	last := starts[len(starts)-1]
	return append([]Message{history[0]}, history[last:]...), last - 1
}

// Trimming alone forgets: a fact from early in a long chat just disappears.
// A Summarizer instead asks the model to condense the turns that no longer
// fit into a short summary, which is sent along with the recent turns.

const (
	// summaryReserve is part of the token budget kept free for the summary.
	summaryReserve = 400
	// Limits on what we feed the summarizing call, so it can't overflow either.
	maxSummaryMsgChars   = 600
	maxSummaryInputChars = 12000
)

// Summarizer remembers the summary between calls, so each turn only pays for
// summarizing what newly fell out of the window.
type Summarizer struct {
	chat   func([]Message) (Message, error) // one plain model call, no tools
	text   string                           // the running summary
	covers int                              // how many messages after the system message it accounts for
}

// View returns the messages to send and how many old messages were left out.
// Those are replaced by a summary appended to the system message, so the
// history still reads system, user, assistant, ... as models expect.
//
// If summarizing fails, View returns the plain trimmed view along with the
// error: the turn can still go ahead, just without the summary.
// A nil Summarizer simply trims.
func (s *Summarizer) View(history []Message, budget int) ([]Message, int, error) {
	if s == nil || budget <= 0 {
		view, dropped := trimHistory(history, budget)
		return view, dropped, nil
	}

	// Leave room for the summary itself. (Never go to 0: that means "no limit".)
	view, dropped := trimHistory(history, max(budget-summaryReserve, 1))
	if dropped == 0 {
		return history, 0, nil
	}

	if s.covers > dropped { // shouldn't happen as history only grows; start over to be safe
		s.text, s.covers = "", 0
	}
	if s.covers < dropped {
		text, err := s.summarize(history[1+s.covers : 1+dropped])
		if err != nil {
			return view, dropped, err
		}
		s.text, s.covers = text, dropped
	}

	withSummary := append([]Message(nil), view...)
	withSummary[0].Content += "\n\nSummary of the earlier conversation (those messages are no longer shown):\n" + s.text
	return withSummary, dropped, nil
}

// summarize folds msgs into the running summary with one model call.
func (s *Summarizer) summarize(msgs []Message) (string, error) {
	fmt.Printf("[summarizing %d older messages...]\n", len(msgs))

	transcript := renderTranscript(msgs)
	prompt := "Previous summary:\n" + orNone(s.text) + "\n\nNew messages:\n" + transcript +
		"\n\nWrite an updated summary of the whole conversation in under 150 words. " +
		"Keep names, facts, decisions, file names and unfinished tasks. Leave out pleasantries."

	reply, err := s.chat([]Message{
		{Role: "system", Content: "You condense conversations into short summaries. Reply with the summary only."},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	text := strings.TrimSpace(reply.Content)
	if text == "" {
		return "", fmt.Errorf("summarize: model returned an empty summary")
	}
	return text, nil
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// renderTranscript turns messages into plain text for the summarizer. Long
// messages (file contents, command output) are cut short, and if the whole
// thing is still too long only the most recent part is kept.
func renderTranscript(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		text := m.Content
		for _, tc := range m.ToolCalls {
			text += fmt.Sprintf(" [called %s %v]", tc.Function.Name, tc.Function.Arguments)
		}
		if len(text) > maxSummaryMsgChars {
			text = text[:maxSummaryMsgChars] + "..."
		}
		fmt.Fprintf(&b, "%s: %s\n", m.Role, text)
	}
	out := b.String()
	if len(out) > maxSummaryInputChars {
		out = "...\n" + out[len(out)-maxSummaryInputChars:]
	}
	return out
}
