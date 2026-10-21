package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Long-term memory is a JSON file of short notes the agent chooses to keep.
// Unlike the session file (the whole conversation, replaced by -new), notes
// persist across every conversation until the file is deleted or edited.

// Note is one remembered fact.
type Note struct {
	Text  string    `json:"text"`
	Saved time.Time `json:"saved"`
}

func loadNotes(path string) ([]Note, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	var notes []Note
	if err := json.Unmarshal(data, &notes); err != nil {
		return nil, fmt.Errorf("parse memory %s: %w", path, err)
	}
	return notes, nil
}

// Remember saves a note to the memory file.
type Remember struct{ Path string }

func (Remember) Name() string { return "remember" }
func (Remember) Description() string {
	return "Save a short fact to long-term memory so it is available in future conversations. " +
		"Use it for lasting things the user tells you about themselves or their preferences."
}
func (Remember) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"note": map[string]any{"type": "string", "description": "The fact to remember, as one self-contained sentence"},
		},
		"required": []string{"note"},
	}
}
func (r Remember) Run(args map[string]any) (string, error) {
	note, ok := args["note"].(string)
	note = strings.TrimSpace(note)
	if !ok || note == "" {
		return "", fmt.Errorf("missing string argument 'note'")
	}
	notes, err := loadNotes(r.Path)
	if err != nil {
		return "", err
	}
	for _, n := range notes {
		if strings.EqualFold(n.Text, note) {
			return "Already remembered.", nil
		}
	}
	notes = append(notes, Note{Text: note, Saved: time.Now()})
	if err := writeJSONAtomic(r.Path, notes); err != nil {
		return "", err
	}
	return "Remembered: " + note, nil
}

// Recall searches the notes saved by Remember.
type Recall struct{ Path string }

func (Recall) Name() string { return "recall" }
func (Recall) Description() string {
	return "Look up facts saved in long-term memory. Give a keyword to filter, or leave it empty to list everything (best when unsure). " +
		"Use it when a question might depend on something the user told you earlier."
}
func (Recall) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Keyword to search for; empty returns all notes"},
		},
	}
}
func (r Recall) Run(args map[string]any) (string, error) {
	query, _ := args["query"].(string) // optional: a missing or wrong-typed query means "all"
	query = strings.ToLower(strings.TrimSpace(query))

	notes, err := loadNotes(r.Path)
	if err != nil {
		return "", err
	}
	if len(notes) == 0 {
		return "No notes saved yet.", nil
	}

	var found, all []string
	for _, n := range notes {
		line := "- " + n.Text
		all = append(all, line)
		if query == "" || strings.Contains(strings.ToLower(n.Text), query) {
			found = append(found, line)
		}
	}
	if len(found) > 0 {
		return truncate(strings.Join(found, "\n")), nil
	}
	// Plain substring matching misses meaning ("city" won't match "Austin").
	// The note list is small, so on a miss hand everything to the model and
	// let it do the semantic matching.
	return truncate("No note contains " + fmt.Sprintf("%q", query) + ". All notes:\n" + strings.Join(all, "\n")), nil
}
