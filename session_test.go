package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSessionRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "session.json") // sub dir must be created
	call := ToolCall{}
	call.Function.Name = "read_file"
	call.Function.Arguments = map[string]any{"path": "go.mod"}
	want := []Message{
		{Role: "user", Content: "read go.mod"},
		{Role: "assistant", ToolCalls: []ToolCall{call}},
		{Role: "tool", Content: "module go-harness", ToolName: "read_file"},
		{Role: "assistant", Content: "Done."},
	}

	if err := saveSession(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Fatal("temp file left behind")
	}
}

func TestLoadSession(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{nope"), 0o600)

	if msgs, err := loadSession(filepath.Join(dir, "missing.json")); err != nil || msgs != nil {
		t.Fatalf("missing file: msgs = %v, err = %v; want nil, nil", msgs, err)
	}
	if _, err := loadSession(bad); err == nil || !strings.Contains(err.Error(), "-new") {
		t.Fatalf("corrupt file: err = %v, want a hint about -new", err)
	}
}

func TestOpenSession(t *testing.T) {
	old := []Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	setup := func(t *testing.T, content string) string {
		path := filepath.Join(t.TempDir(), "session.json")
		if content != "" {
			os.WriteFile(path, []byte(content), 0o600)
		}
		return path
	}

	t.Run("resumes the saved chat", func(t *testing.T) {
		path := setup(t, "")
		saveSession(path, old)
		var out bytes.Buffer
		got, err := openSession(path, false, &out)
		if err != nil || len(got) != 2 || !strings.Contains(out.String(), "Resumed 2 messages") {
			t.Fatalf("got %v, %v, output %q", got, err, out.String())
		}
	})

	t.Run("no saved chat: nothing to say", func(t *testing.T) {
		var out bytes.Buffer
		got, err := openSession(setup(t, ""), false, &out)
		if err != nil || got != nil || out.Len() != 0 {
			t.Fatalf("got %v, %v, output %q", got, err, out.String())
		}
	})

	t.Run("fresh empties the file immediately", func(t *testing.T) {
		path := setup(t, "")
		saveSession(path, old)
		var out bytes.Buffer
		got, err := openSession(path, true, &out)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v", got, err)
		}
		// The point of the change: even if the user quits now, the old chat is gone.
		if again, _ := loadSession(path); len(again) != 0 {
			t.Fatalf("old chat still on disk: %v", again)
		}
		if !strings.Contains(out.String(), "Started a new conversation") {
			t.Fatalf("output = %q, want a confirmation", out.String())
		}
	})

	t.Run("fresh also repairs a corrupt file", func(t *testing.T) {
		path := setup(t, "{nope")
		if _, err := openSession(path, true, io.Discard); err != nil {
			t.Fatalf("-new should replace a corrupt session, got %v", err)
		}
		if _, err := loadSession(path); err != nil {
			t.Fatalf("file should be valid now: %v", err)
		}
	})

	t.Run("corrupt file without fresh is an error that points at -new", func(t *testing.T) {
		_, err := openSession(setup(t, "{nope"), false, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "-new") {
			t.Fatalf("err = %v", err)
		}
	})
}
