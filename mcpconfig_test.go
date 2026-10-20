package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMCPConfig(t *testing.T) {
	write := func(content string) string {
		p := filepath.Join(t.TempDir(), "mcp.json")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	missing := filepath.Join(t.TempDir(), "nope.json")

	tests := []struct {
		name     string
		path     string
		optional bool
		wantErr  string // substring; empty means success
		wantCmd  string // command of server "fs", if success
	}{
		{"valid", write(`{"mcpServers":{"fs":{"command":"npx","args":["-y","x"]}}}`), false, "", "npx"},
		{"missing but optional", missing, true, "", ""},
		{"missing and required", missing, false, "read MCP config", ""},
		{"bad json", write(`{nope`), false, "parse", ""},
		{"no command", write(`{"mcpServers":{"fs":{"args":["x"]}}}`), false, `server "fs" has no command`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadMCPConfig(tt.path, tt.optional)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got["fs"].Command != tt.wantCmd {
				t.Fatalf("fs command = %q, want %q", got["fs"].Command, tt.wantCmd)
			}
		})
	}
}

func TestConnectMCPServers(t *testing.T) {
	t.Setenv("GO_FAKE_MCP_SERVER", "1")
	servers := map[string]MCPServerConfig{
		"fake": {Command: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}},
		"gone": {Command: "definitely-not-a-real-command"}, // must be skipped, not fatal
	}
	tools, clients := connectMCPServers(servers)
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	if len(clients) != 1 || len(tools) != 2 {
		t.Fatalf("got %d clients, %d tools; want 1 and 2", len(clients), len(tools))
	}
	if tools[0].Name() != "fake__echo" {
		t.Fatalf("first tool = %q", tools[0].Name())
	}
}
