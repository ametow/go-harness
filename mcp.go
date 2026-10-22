package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// mcpProtocolVersion is the MCP revision we speak. The server replies with
// the version it will use, which may be older.
const mcpProtocolVersion = "2025-06-18"

// MCPClient talks to one MCP server that we launch as a child process.
// MCP over stdio is JSON-RPC 2.0: each message is a single line of JSON,
// written to the server's stdin and read from its stdout.
//
// One goroutine (readLoop) owns stdout and hands each reply to the call that
// is waiting for it, matched by request id. That lets a call stop waiting at
// any time, because of a timeout or a Ctrl-C, without disturbing the
// connection: if its reply shows up later, nobody is waiting and it is dropped.
type MCPClient struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex // one write at a time, so concurrent calls don't interleave lines

	mu      sync.Mutex // guards nextID and pending
	nextID  int
	pending map[int]chan rpcMessage // calls waiting for a reply, by request id

	done    chan struct{} // closed when the server's output ends
	readErr error         // why it ended; only valid once done is closed

	// timeout bounds how long we wait for any one reply, so a hung server
	// can't freeze the whole agent.
	timeout time.Duration

	// closeTimeout is how long Close waits for the server to exit by itself
	// before killing it.
	closeTimeout time.Duration
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
	detachFromTerminal(cmd)

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
	c := &MCPClient{
		cmd:          cmd,
		stdin:        stdin,
		pending:      make(map[int]chan rpcMessage),
		done:         make(chan struct{}),
		timeout:      30 * time.Second,
		closeTimeout: 2 * time.Second,
	}
	go c.readLoop(bufio.NewReader(stdout))
	return c, nil
}

// Initialize performs the MCP handshake: send "initialize", read the server's
// answer, then send the "initialized" notification. Nothing else works before this.
func (c *MCPClient) Initialize(ctx context.Context) (ServerInfo, error) {
	result, err := c.call(ctx, "initialize", map[string]any{
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
// "we're done" for stdio servers) and waiting for it to exit. A server that
// ignores this is killed after closeTimeout, so quitting can never hang.
func (c *MCPClient) Close() error {
	c.stdin.Close()

	// Wait for the read loop first: cmd.Wait closes the output pipe, which must
	// not happen while another goroutine is still reading it.
	select {
	case <-c.done:
	case <-time.After(c.closeTimeout):
		c.cmd.Process.Kill()
		<-c.done
	}

	exited := make(chan error, 1)
	go func() { exited <- c.cmd.Wait() }()
	select {
	case err := <-exited:
		return err
	case <-time.After(c.closeTimeout):
		c.cmd.Process.Kill()
		return <-exited
	}
}

// readLoop reads the server's output until it ends, passing each reply to the
// call waiting for it.
func (c *MCPClient) readLoop(r *bufio.Reader) {
	defer close(c.done)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			c.readErr = err
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: MCP server sent a line that is not JSON: %.80q\n", line)
			continue
		}
		// Notifications (like log messages) and requests from the server
		// have a method; we don't act on them.
		if msg.Method != "" || msg.ID == nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[*msg.ID]
		delete(c.pending, *msg.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- msg // buffered with room for exactly this one reply, so it never blocks
		}
		// No waiting call means it was cancelled or timed out: drop the reply.
	}
}

// call sends a request and waits for its reply. It gives up when ctx is
// cancelled or after c.timeout. Giving up leaves the connection usable.
func (c *MCPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	ch := make(chan rpcMessage, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}

	if err := c.send(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		forget()
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	select {
	case msg := <-ch:
		return replyResult(method, msg)

	case <-c.done:
		forget()
		select { // a reply may have arrived just before the output ended
		case msg := <-ch:
			return replyResult(method, msg)
		default:
		}
		return nil, fmt.Errorf("%s: the server closed its output (did it exit?): %w", method, c.readErr)

	case <-ctx.Done():
		forget()
		// Tell the server it can stop working on it. (The spec forbids
		// cancelling "initialize".)
		if method != "initialize" {
			c.send(rpcRequest{JSONRPC: "2.0", Method: "notifications/cancelled",
				Params: map[string]any{"requestId": id, "reason": "the client stopped waiting"}})
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%s: no reply from server after %s", method, c.timeout)
		}
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

func replyResult(method string, msg rpcMessage) (json.RawMessage, error) {
	if msg.Error != nil {
		return nil, fmt.Errorf("%s: server error %d: %s", method, msg.Error.Code, msg.Error.Message)
	}
	return msg.Result, nil
}

func (c *MCPClient) send(req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode %s: %w", req.Method, err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
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
// so the rest of the harness can treat them like the built-in ones. prefix is
// added to each name the model sees (e.g. "fs__") so tools from different
// servers, or built-ins, can't collide; the server still gets the original name.
func (c *MCPClient) ListTools(ctx context.Context, prefix string) ([]Tool, error) {
	var tools []Tool
	cursor := ""
	for {
		// Big tool lists come in pages; a non-empty nextCursor means "ask again".
		var params map[string]any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		result, err := c.call(ctx, "tools/list", params)
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
			tools = append(tools, &mcpTool{client: c, name: prefix + info.Name, info: info})
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
	name   string // what the model sees (prefixed); info.Name is what the server knows
	info   mcpToolInfo
}

func (t *mcpTool) Name() string        { return t.name }
func (t *mcpTool) Description() string { return t.info.Description }
func (t *mcpTool) Parameters() map[string]any {
	if t.info.InputSchema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return t.info.InputSchema
}

// NeedsConfirmation makes every MCP tool ask first: we know nothing about what
// a third-party server's tool does, so the user decides each time.
func (t *mcpTool) NeedsConfirmation(args map[string]any) string {
	return fmt.Sprintf("MCP tool %s %v", t.name, args)
}

// Run asks the server to execute the tool (tools/call) and returns its text.
func (t *mcpTool) Run(ctx context.Context, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{} // the protocol wants an object, never null
	}
	result, err := t.client.call(ctx, "tools/call", map[string]any{"name": t.info.Name, "arguments": args})
	if err != nil {
		return "", err
	}

	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"` // the tool ran but failed; distinct from a protocol error
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return "", fmt.Errorf("decode tools/call result: %w", err)
	}

	var parts []string
	for _, c := range res.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		} else {
			parts = append(parts, fmt.Sprintf("[%s content omitted]", c.Type)) // images etc. aren't supported yet
		}
	}
	text := truncate(strings.Join(parts, "\n"))
	if res.IsError {
		return "", errors.New(text)
	}
	return text, nil
}
