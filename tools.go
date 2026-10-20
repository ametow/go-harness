package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// maxOutput caps what a tool returns, since everything goes back into the
// model's limited context window.
const maxOutput = 8000

// bashTimeout stops a hung command from blocking the agent forever.
const bashTimeout = 15 * time.Second

func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + fmt.Sprintf("\n[truncated: %d more bytes]", len(s)-maxOutput)
}

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

// Confirmable is implemented by tools that can change things (run commands,
// write files). NeedsConfirmation returns a short description of the exact
// action, which is shown to the user before they approve it.
type Confirmable interface {
	NeedsConfirmation(args map[string]any) string
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

// Bash is a tool that runs a shell command and returns its output.
type Bash struct{}

func (Bash) Name() string        { return "bash" }
func (Bash) Description() string { return "Run a shell command and return its output." }
func (Bash) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command to run"},
		},
		"required": []string{"command"},
	}
}
func (Bash) NeedsConfirmation(args map[string]any) string {
	return fmt.Sprintf("bash command: %v", args["command"])
}
func (Bash) Run(args map[string]any) (string, error) {
	cmd, ok := args["command"].(string)
	if !ok || cmd == "" {
		return "", fmt.Errorf("missing string argument 'command'")
	}
	return runShellCommand(cmd), nil
}

// runShellCommand returns the combined output. A failing command is not a Go
// error: the model needs to see the output and exit status to fix its command.
func runShellCommand(cmd string) string {
	ctx, cancel := context.WithTimeout(context.Background(), bashTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "bash", "-c", cmd).CombinedOutput()
	result := truncate(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		return result + fmt.Sprintf("\n[killed after %s]", bashTimeout)
	}
	if err != nil {
		return result + fmt.Sprintf("\n[command failed: %v]", err)
	}
	return result
}

// ListDirectoryFiles returns the list of files and folders in the given directory.
type ListDirectoryFiles struct{}

func (this ListDirectoryFiles) Name() string {
	return "list_directory"
}

func (this ListDirectoryFiles) Description() string {
	return "List the files and folders in a directory."
}

func (this ListDirectoryFiles) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path to the directory"},
		},
		"required": []string{"path"},
	}
}

func (this ListDirectoryFiles) Run(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("missing string argument 'path'")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	var result []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/" // lets the model tell folders from files
		}
		result = append(result, name)
	}
	return fmt.Sprintf("Files in %s: %v", path, result), nil
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
	return truncate(string(data)), nil
}
