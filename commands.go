package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Slash commands are handled by the program itself and never sent to the
// model, so a mistyped "/new" costs no model call.

// chatState is everything a command may need to look at or change.
type chatState struct {
	cfg        Config
	history    []Message // history[0] is the system message
	summarizer *Summarizer
	out        io.Writer
}

type command struct {
	name string
	help string
	run  func(s *chatState) (quit bool)
}

// commands lists them in the order shown by /help and in suggestions.
// (A slice, not a map, to keep that order. It is set up in init because the
// help command refers back to the list.)
var commands []command

func init() {
	commands = []command{
		{"/help", "List the commands", cmdHelp},
		{"/new", "Start a new conversation (notes are kept)", cmdNew},
		{"/clean", "Remove empty replies from the history", cmdClean},
		{"/model", "Show models and conversation size", cmdModel},
		{"/notes", "Show the saved long-term notes", cmdNotes},
		{"/forget", "Delete all long-term notes", cmdForget},
		{"/exit", "Quit", func(*chatState) bool { return true }},
	}
}

// isCommandLine tells commands from ordinary messages. A message like
// "/etc/hosts what is this?" starts with "/" but is a path, not a command.
func isCommandLine(line string) bool {
	fields := strings.Fields(line)
	return len(fields) > 0 && isCommandPrefix(fields[0])
}

// runCommand runs a command line. It reports whether to quit.
func (s *chatState) runCommand(line string) (quit bool) {
	name := strings.Fields(line)[0]
	for _, c := range commands {
		if c.name == name {
			return c.run(s)
		}
	}
	fmt.Fprintf(s.out, "Unknown command %s. Type /help to see the commands.\n", name)
	return false
}

// matchCommands and describeCommand feed the line editor's suggestions.
func matchCommands(prefix string) []string {
	var out []string
	for _, c := range commands {
		if strings.HasPrefix(c.name, prefix) {
			out = append(out, c.name)
		}
	}
	return out
}

func describeCommand(name string) string {
	for _, c := range commands {
		if c.name == name {
			return c.help
		}
	}
	return ""
}

func cmdHelp(s *chatState) bool {
	for _, c := range commands {
		fmt.Fprintf(s.out, "  %-8s %s\n", c.name, c.help)
	}
	fmt.Fprintln(s.out, "Type / and press Tab to complete a command.")
	return false
}

func cmdNew(s *chatState) bool {
	s.history = s.history[:1] // keep only the system message
	s.summarizer.Reset()
	s.save()
	fmt.Fprintln(s.out, "Started a new conversation. Long-term notes were kept.")
	return false
}

func cmdClean(s *chatState) bool {
	cleaned, removed := cleanHistory(s.history)
	s.history = cleaned
	s.summarizer.Reset() // it counts messages by position, which just changed
	s.save()
	fmt.Fprintf(s.out, "Removed %d messages.\n", removed)
	return false
}

// cleanHistory drops empty assistant replies, and the user message each one
// failed to answer, so the model isn't shown a pattern of saying nothing.
func cleanHistory(msgs []Message) ([]Message, int) {
	out := make([]Message, 0, len(msgs))
	removed := 0
	for _, m := range msgs {
		if m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) == 0 {
			removed++
			if n := len(out); n > 0 && out[n-1].Role == "user" {
				out = out[:n-1]
				removed++
			}
			continue
		}
		out = append(out, m)
	}
	return out, removed
}

func cmdModel(s *chatState) bool {
	fast := s.cfg.FastModel
	if fast == "" {
		fast = "none (routing off)"
	}
	fmt.Fprintf(s.out, "  main model:  %s\n  fast model:  %s\n", s.cfg.Model, fast)
	fmt.Fprintf(s.out, "  history:     %d messages, about %d tokens (budget %d)\n",
		len(s.history)-1, estimateTokens(s.history), s.cfg.ContextTokens)
	if s.summarizer != nil && s.summarizer.text != "" {
		fmt.Fprintf(s.out, "  summary:     covers the %d oldest messages\n", s.summarizer.covers)
	}
	return false
}

func cmdNotes(s *chatState) bool {
	notes, err := loadNotes(s.cfg.Memory)
	if err != nil {
		fmt.Fprintln(s.out, "error:", err)
		return false
	}
	if len(notes) == 0 {
		fmt.Fprintln(s.out, "No notes saved.")
		return false
	}
	for _, n := range notes {
		fmt.Fprintf(s.out, "  - %s (%s)\n", n.Text, n.Saved.Format("2006-01-02"))
	}
	return false
}

func cmdForget(s *chatState) bool {
	notes, err := loadNotes(s.cfg.Memory)
	if err != nil {
		fmt.Fprintln(s.out, "error:", err)
		return false
	}
	if len(notes) == 0 {
		fmt.Fprintln(s.out, "No notes saved.")
		return false
	}
	if !confirm(fmt.Sprintf("deleting all %d notes from %s", len(notes), s.cfg.Memory)) {
		fmt.Fprintln(s.out, "Kept the notes.")
		return false
	}
	if err := os.Remove(s.cfg.Memory); err != nil {
		fmt.Fprintln(s.out, "error:", err)
		return false
	}
	fmt.Fprintln(s.out, "Deleted all notes.")
	return false
}

// save writes the conversation to the session file, minus the system message.
func (s *chatState) save() {
	if err := saveSession(s.cfg.Session, s.history[1:]); err != nil {
		fmt.Fprintln(s.out, "warning: could not save session:", err)
	}
}
