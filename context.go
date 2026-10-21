package main

import "fmt"

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
