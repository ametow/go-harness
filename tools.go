package main

import (
	"fmt"
	"os"
	"time"
)

// Tool is something the model can ask us to run. The model never executes
// anything itself: it only emits a name and arguments, and we do the work.
type Tool interface {
	Name() string
	Description() string
	// Parameters is a JSON Schema describing the arguments, sent to the model
	// so it knows how to call the tool.
	Parameters() map[string]any
	Run(args map[string]any) (string, error)
}

// findTool looks up a tool by name; the model may ask for one that doesn't exist.
func findTool(tools []Tool, name string) (Tool, bool) {
	for _, t := range tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

// CurrentTime returns the local time. The model can't know this on its own.
type CurrentTime struct{}

func (CurrentTime) Name() string        { return "current_time" }
func (CurrentTime) Description() string { return "Get the current local date and time." }
func (CurrentTime) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (CurrentTime) Run(map[string]any) (string, error) {
	return time.Now().Format(time.RFC1123), nil
}

// ReadFile returns the contents of a file on disk.
type ReadFile struct{}

func (ReadFile) Name() string        { return "read_file" }
func (ReadFile) Description() string { return "Read a text file and return its contents." }
func (ReadFile) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path to the file"},
		},
		"required": []string{"path"},
	}
}
func (ReadFile) Run(args map[string]any) (string, error) {
	// Model-supplied arguments are untrusted input, so check the type.
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("missing string argument 'path'")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}
