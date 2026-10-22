package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestListDirectoryFiles_Run(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("one"), 0o600); err != nil {
		t.Fatalf("write alpha: %v", err)
	}
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("make nested dir: %v", err)
	}

	result, err := ListDirectoryFiles{}.Run(context.Background(), map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !strings.Contains(result, "alpha.txt") {
		t.Fatalf("result %q missing file entry", result)
	}
	if !strings.Contains(result, "nested/") {
		t.Fatalf("result %q missing directory entry", result)
	}
}

func TestListDirectoryFiles_RequiresPath(t *testing.T) {
	_, err := ListDirectoryFiles{}.Run(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestTruncate(t *testing.T) {
	short := strings.Repeat("a", maxOutput)
	if got := truncate(short); got != short {
		t.Fatalf("string at the limit should be unchanged")
	}
	long := strings.Repeat("a", maxOutput+5)
	got := truncate(long)
	if !strings.HasPrefix(got, short) || !strings.Contains(got, "[truncated: 5 more bytes]") {
		t.Fatalf("unexpected truncation result: %q", got[len(got)-40:])
	}
}

func TestRunShellCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{"success", "echo hi", "hi"},
		{"failure keeps output", "echo oops; exit 3", "[command failed: exit status 3]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runShellCommand(context.Background(), tt.cmd); !strings.Contains(got, tt.want) {
				t.Fatalf("output %q missing %q", got, tt.want)
			}
		})
	}
}

func TestBashCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs bash and process groups")
	}
	tests := []struct {
		name string
		cmd  string
	}{
		{"simple command", "sleep 5"},
		// The subshell is a child of bash; if only bash were killed, it would
		// keep running and keep the output pipe open.
		{"command that started another process", "(sleep 5; echo late) & wait"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(100*time.Millisecond, cancel)

			start := time.Now()
			out := runShellCommand(ctx, tt.cmd)

			if !strings.Contains(out, "[cancelled]") {
				t.Fatalf("output = %q, want it to say the command was cancelled", out)
			}
			if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
				t.Fatalf("took %s: the whole process group should be killed at once", elapsed)
			}
		})
	}
}
