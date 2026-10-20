package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// mcpProtocolVersion is the MCP revision we speak. The server replies with
// the version it will use, which may be older.
const mcpProtocolVersion = "2025-06-18"

// MCPClient talks to one MCP server that we launch as a child process.
// MCP over stdio is JSON-RPC 2.0: each message is a single line of JSON,
// written to the server's stdin and read from its stdout.
type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int
}

// rpcRequest is a JSON-RPC request. A notification is the same thing without
// an ID: the server sends no reply to it.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcMessage is anything the server sends us: a response (has ID, no Method),
// or a notification/request from the server (has Method).
type rpcMessage struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServerInfo is what a server tells us about itself during the handshake.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// StartMCP launches the server command. Call Initialize next, and Close when done.
func StartMCP(command string, args ...string) (*MCPClient, error) {
	cmd := exec.Command(command, args...)
	cmd.Stderr = os.Stderr // servers log to stderr; let the user see it

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}
	return &MCPClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}, nil
}

// Initialize performs the MCP handshake: send "initialize", read the server's
// answer, then send the "initialized" notification. Nothing else works before this.
func (c *MCPClient) Initialize() (ServerInfo, error) {
	result, err := c.call("initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{}, // we offer no client features yet
		"clientInfo":      map[string]any{"name": "go-harness", "version": "0.1"},
	})
	if err != nil {
		return ServerInfo{}, err
	}

	var res struct {
		ServerInfo ServerInfo `json:"serverInfo"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return ServerInfo{}, fmt.Errorf("decode initialize result: %w", err)
	}

	if err := c.send(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return ServerInfo{}, err
	}
	return res.ServerInfo, nil
}

// Close shuts the server down by closing its stdin (the polite way to say
// "we're done" for stdio servers), then waits for it to exit.
func (c *MCPClient) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}

// call sends a request and waits for the response with the matching ID.
func (c *MCPClient) call(method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := c.send(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		return nil, err
	}

	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("read %s response (did the server exit?): %w", method, err)
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil, fmt.Errorf("decode message %q: %w", line, err)
		}
		// Servers may send notifications (e.g. log messages) at any time.
		// Skip anything that isn't the reply to our request.
		if msg.Method != "" || msg.ID == nil || *msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: server error %d: %s", method, msg.Error.Code, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

func (c *MCPClient) send(req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode %s: %w", req.Method, err)
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", req.Method, err)
	}
	return nil
}

// mcpToolInfo is one tool as described by a server's tools/list reply.
type mcpToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"` // JSON Schema, same shape Ollama wants
}

// ListTools asks the server what tools it offers and returns them as Tools,
// so the rest of the harness can treat them like the built-in ones.
func (c *MCPClient) ListTools() ([]Tool, error) {
	var tools []Tool
	cursor := ""
	for {
		// Big tool lists come in pages; a non-empty nextCursor means "ask again".
		var params map[string]any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		result, err := c.call("tools/list", params)
		if err != nil {
			return nil, err
		}

		var page struct {
			Tools      []mcpToolInfo `json:"tools"`
			NextCursor string        `json:"nextCursor"`
		}
		if err := json.Unmarshal(result, &page); err != nil {
			return nil, fmt.Errorf("decode tools/list result: %w", err)
		}
		for _, info := range page.Tools {
			tools = append(tools, &mcpTool{client: c, info: info})
		}

		if page.NextCursor == "" {
			return tools, nil
		}
		cursor = page.NextCursor
	}
}

// mcpTool adapts a server-side tool to our Tool interface.
type mcpTool struct {
	client *MCPClient
	info   mcpToolInfo
}

func (t *mcpTool) Name() string        { return t.info.Name }
func (t *mcpTool) Description() string { return t.info.Description }
func (t *mcpTool) Parameters() map[string]any {
	if t.info.InputSchema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return t.info.InputSchema
}

// Run is a placeholder: calling the tool (tools/call) comes in step 8c.
func (t *mcpTool) Run(map[string]any) (string, error) {
	return "", fmt.Errorf("MCP tool %q cannot be called yet", t.info.Name)
}
