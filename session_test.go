package main

import (
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
