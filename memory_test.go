package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRememberAndRecall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	remember, recall := Remember{path}, Recall{path}

	for _, note := range []string{"The user's favorite color is teal", "The user lives in Austin", "the user lives in austin"} {
		if _, err := remember.Run(context.Background(), map[string]any{"note": note}); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name  string
		args  map[string]any
		want  []string
		avoid []string
	}{
		{"all notes, deduplicated", map[string]any{}, []string{"teal", "Austin"}, nil},
		{"filter ignores case", map[string]any{"query": "COLOR"}, []string{"teal"}, []string{"Austin"}},
		{"miss falls back to everything", map[string]any{"query": "dog"}, []string{`No note contains "dog"`, "teal", "Austin"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := recall.Run(context.Background(), tt.args)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("result %q missing %q", got, w)
				}
			}
			for _, a := range tt.avoid {
				if strings.Contains(got, a) {
					t.Errorf("result %q should not contain %q", got, a)
				}
			}
		})
	}

	// The near-duplicate ("the user lives in austin") must not have been saved twice.
	notes, _ := loadNotes(path)
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want 2: %+v", len(notes), notes)
	}
}

func TestRememberRequiresNote(t *testing.T) {
	r := Remember{filepath.Join(t.TempDir(), "memory.json")}
	for _, args := range []map[string]any{{}, {"note": "  "}, {"note": 5}} {
		if _, err := r.Run(context.Background(), args); err == nil {
			t.Fatalf("expected error for args %v", args)
		}
	}
}

func TestRecallEmptyMemory(t *testing.T) {
	got, err := Recall{filepath.Join(t.TempDir(), "memory.json")}.Run(context.Background(), map[string]any{})
	if err != nil || got != "No notes saved yet." {
		t.Fatalf("got %q, err %v", got, err)
	}
}
