package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// loadSession reads a saved conversation. A missing file is not an error:
// it just means there is nothing to resume yet.
func loadSession(path string) ([]Message, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("parse session %s (start fresh with -new): %w", path, err)
	}
	return msgs, nil
}

// saveSession writes the conversation as JSON.
func saveSession(path string, msgs []Message) error {
	return writeJSONAtomic(path, msgs)
}

// writeJSONAtomic writes v as indented JSON. It writes to a temp file and
// renames it, so a crash mid-write can't leave a half-written file behind.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
