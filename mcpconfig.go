package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
)

// MCPServerConfig says how to launch one MCP server. The file format matches
// the "mcpServers" convention other MCP clients use, so snippets copy over:
//
//	{"mcpServers": {"fs": {"command": "npx", "args": ["-y", "some-server", "/path"]}}}
type MCPServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// loadMCPConfig reads the servers from a JSON file. If optional is true, a
// missing file just means "no servers" (the default mcp.json need not exist);
// a file the user named explicitly must exist.
func loadMCPConfig(path string, optional bool) (map[string]MCPServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read MCP config: %w", err)
	}

	var file struct {
		Servers map[string]MCPServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for name, srv := range file.Servers {
		if srv.Command == "" {
			return nil, fmt.Errorf("%s: server %q has no command", path, name)
		}
	}
	return file.Servers, nil
}

// connectMCPServers starts every configured server, does the handshake and
// collects their tools, each named "<server>__<tool>". A server that fails is
// reported and skipped: one broken server shouldn't stop the agent starting.
// The caller must Close every returned client.
func connectMCPServers(ctx context.Context, servers map[string]MCPServerConfig) ([]Tool, []*MCPClient) {
	// Sorted, so tool order (and what the model sees) is the same every run.
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	var tools []Tool
	var clients []*MCPClient
	for _, name := range names {
		srv := servers[name]
		client, err := StartMCP(srv.Command, srv.Args...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: MCP server %q: %v\n", name, err)
			continue
		}
		serverTools, err := initAndList(ctx, client, name+"__")
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: MCP server %q: %v\n", name, err)
			client.Close()
			continue
		}
		fmt.Printf("MCP server %q: %d tools\n", name, len(serverTools))
		tools = append(tools, serverTools...)
		clients = append(clients, client)
	}
	return tools, clients
}

func initAndList(ctx context.Context, c *MCPClient, prefix string) ([]Tool, error) {
	if _, err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	return c.ListTools(ctx, prefix)
}
