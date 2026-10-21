package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newState(t *testing.T) (*chatState, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	var out bytes.Buffer
	return &chatState{
		cfg: Config{
			Model: "big", FastModel: "small", ContextTokens: 4000,
			Session: filepath.Join(dir, "session.json"),
			Memory:  filepath.Join(dir, "memory.json"),
		},
		history:    []Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}},
		summarizer: &Summarizer{text: "old summary", covers: 4},
		out:        &out,
	}, &out
}

func TestIsCommandLine(t *testing.T) {
	for line, want := range map[string]bool{
		"/new":                  true,
		"/new please":           true,
		"/":                     true,
		"/etc/hosts what is it": false, // a path
		"hello /new":            false,
		"what is 6/3?":          false,
		"":                      false,
	} {
		if got := isCommandLine(line); got != want {
			t.Errorf("isCommandLine(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestCommandNew(t *testing.T) {
	s, out := newState(t)
	if quit := s.runCommand("/new"); quit {
		t.Fatal("/new must not quit")
	}
	if len(s.history) != 1 || s.history[0].Role != "system" {
		t.Fatalf("history = %+v, want only the system message", s.history)
	}
	if s.summarizer.text != "" || s.summarizer.covers != 0 {
		t.Fatal("summary should be reset")
	}
	saved, err := loadSession(s.cfg.Session)
	if err != nil || len(saved) != 0 {
		t.Fatalf("session file = %v, %v; want an empty conversation", saved, err)
	}
	if !strings.Contains(out.String(), "notes were kept") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCleanHistory(t *testing.T) {
	call := ToolCall{}
	call.Function.Name = "add"
	in := []Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "hi"}, {Role: "assistant", Content: ""}, // failed turn
		{Role: "user", Content: "q2"}, {Role: "assistant", ToolCalls: []ToolCall{call}}, // empty text but a tool call: keep
		{Role: "tool", Content: "3"}, {Role: "assistant", Content: "a2"},
		{Role: "assistant", Content: ""}, // stray empty reply with no user message before it
	}
	got, removed := cleanHistory(in)
	if removed != 3 { // "hi" + its empty reply, and the stray one
		t.Fatalf("removed %d, want 3", removed)
	}
	if len(got) != 7 || got[3].Content != "q2" {
		t.Fatalf("got %+v", got)
	}
}

func TestCommandClean(t *testing.T) {
	s, out := newState(t)
	s.history = append(s.history, Message{Role: "user", Content: "hi"}, Message{Role: "assistant"})
	s.runCommand("/clean")
	if len(s.history) != 3 || s.summarizer.covers != 0 {
		t.Fatalf("history len %d, covers %d", len(s.history), s.summarizer.covers)
	}
	if !strings.Contains(out.String(), "Removed 2 messages") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCommandExitAndUnknown(t *testing.T) {
	s, out := newState(t)
	if !s.runCommand("/exit") {
		t.Fatal("/exit should quit")
	}
	if s.runCommand("/bogus") {
		t.Fatal("unknown commands must not quit")
	}
	if !strings.Contains(out.String(), "Unknown command /bogus") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCommandHelpAndModel(t *testing.T) {
	s, out := newState(t)
	s.runCommand("/help")
	for _, c := range commands {
		if !strings.Contains(out.String(), c.name) {
			t.Errorf("/help does not list %s", c.name)
		}
	}
	out.Reset()
	s.runCommand("/model")
	for _, want := range []string{"big", "small", "2 messages", "covers the 4 oldest"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("/model output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommandNotesAndForget(t *testing.T) {
	s, out := newState(t)
	s.runCommand("/notes")
	if !strings.Contains(out.String(), "No notes saved") {
		t.Fatalf("output = %q", out.String())
	}

	if _, err := (Remember{s.cfg.Memory}).Run(map[string]any{"note": "likes teal"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	s.runCommand("/notes")
	if !strings.Contains(out.String(), "likes teal") {
		t.Fatalf("output = %q", out.String())
	}

	answer := func(a string) {
		old := input
		input = bufio.NewReader(strings.NewReader(a))
		t.Cleanup(func() { input = old })
	}

	answer("n\n") // declined: the file must survive
	s.runCommand("/forget")
	if _, err := os.Stat(s.cfg.Memory); err != nil {
		t.Fatal("notes were deleted although the user said no")
	}

	answer("y\n")
	s.runCommand("/forget")
	if _, err := os.Stat(s.cfg.Memory); !os.IsNotExist(err) {
		t.Fatal("notes should be deleted after yes")
	}
}

func TestMatchCommands(t *testing.T) {
	if got := matchCommands("/n"); len(got) != 2 || got[0] != "/new" || got[1] != "/notes" {
		t.Fatalf("matchCommands(/n) = %v", got)
	}
	if got := matchCommands("/"); len(got) != len(commands) {
		t.Fatalf("matchCommands(/) returned %d, want all %d", len(got), len(commands))
	}
	if got := matchCommands("/zz"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
