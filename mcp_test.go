package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHelperProcess isn't a real test. When the env var is set, the test
// binary runs as a tiny fake MCP server, so we can test the client with no
// external dependencies. (This is the pattern os/exec's own tests use.)
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("GO_FAKE_MCP_SERVER")
	if mode == "" {
		return
	}
	if mode == "stubborn" {
		time.Sleep(time.Hour) // ignores stdin closing, like a misbehaving server
	}
	var cancelled []int // request ids the client told us to stop working on
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				RequestID int    `json:"requestId"`
				Cursor    string `json:"cursor"`
				Name      string `json:"name"`
				Arguments struct {
					Text string `json:"text"`
				} `json:"arguments"`
			} `json:"params"`
		}
		json.Unmarshal(in.Bytes(), &req)
		if req.ID == nil { // a notification: no reply
			if req.Method == "notifications/cancelled" {
				cancelled = append(cancelled, req.Params.RequestID)
			}
			continue
		}
		// A log notification before every reply checks that the client skips it.
		fmt.Println(`{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"hi"}}`)
		switch req.Method {
		case "initialize":
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"9.9"}}}`+"\n", *req.ID)
		case "tools/list":
			// Two pages, to exercise pagination.
			if req.Params.Cursor == "" {
				fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"echo","description":"Echo text back","inputSchema":{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}}],"nextCursor":"page2"}}`+"\n", *req.ID)
			} else {
				fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"ping"}]}}`+"\n", *req.ID)
			}
		case "tools/call":
			if req.Params.Name == "fail" {
				fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"isError":true,"content":[{"type":"text","text":"it broke"}]}}`+"\n", *req.ID)
			} else {
				fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"Echo: %s"},{"type":"image","data":"x"}]}}`+"\n", *req.ID, req.Params.Arguments.Text)
			}
		case "hang":
			time.Sleep(time.Hour) // never reply (select{} would trip Go's deadlock detector and exit)
		case "slow":
			// Replies, but late: after a client that gave up has moved on.
			time.Sleep(300 * time.Millisecond)
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"late":true}}`+"\n", *req.ID)
		case "die":
			os.Exit(3) // the server crashes while a request is waiting
		case "cancelled_ids":
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"ids":%v}}`+"\n", *req.ID, cancelled)
		default:
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"error":{"code":-32601,"message":"method not found"}}`+"\n", *req.ID)
		}
	}
	os.Exit(0)
}

func startFake(t *testing.T) *MCPClient {
	t.Helper()
	t.Setenv("GO_FAKE_MCP_SERVER", "1")
	c, err := StartMCP(os.Args[0], "-test.run=TestHelperProcess")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestMCPInitialize(t *testing.T) {
	c := startFake(t)
	info, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "fake" || info.Version != "9.9" {
		t.Fatalf("server info = %+v", info)
	}
}

func TestMCPServerError(t *testing.T) {
	c := startFake(t)
	_, err := c.call(context.Background(), "bogus", nil)
	if err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("err = %v, want server error", err)
	}
}

func TestStartMCPMissingCommand(t *testing.T) {
	if _, err := StartMCP("definitely-not-a-real-command"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMCPListTools(t *testing.T) {
	c := startFake(t)
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := c.ListTools(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2 (both pages)", len(tools))
	}

	echo, ping := tools[0], tools[1]
	if echo.Name() != "echo" || echo.Description() != "Echo text back" {
		t.Fatalf("echo = %q / %q", echo.Name(), echo.Description())
	}
	if echo.Parameters()["required"] == nil {
		t.Fatalf("echo schema lost: %v", echo.Parameters())
	}
	// A tool with no schema still needs a valid empty one for the model.
	if ping.Parameters()["type"] != "object" {
		t.Fatalf("ping schema = %v", ping.Parameters())
	}

	// It should be usable anywhere a built-in Tool is.
	if d := newToolDef(echo); d.Function.Name != "echo" {
		t.Fatalf("toolDef = %+v", d)
	}
}

func TestMCPToolPrefix(t *testing.T) {
	c := startFake(t)
	tools, err := c.ListTools(context.Background(), "fake__")
	if err != nil {
		t.Fatal(err)
	}
	if tools[0].Name() != "fake__echo" {
		t.Fatalf("name = %q", tools[0].Name())
	}
	// The server must still be called by its own name, not the prefixed one.
	out, err := tools[0].Run(context.Background(), map[string]any{"text": "hi"})
	if err != nil || !strings.HasPrefix(out, "Echo: hi") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

func TestMCPToolRun(t *testing.T) {
	c := startFake(t)
	tests := []struct {
		name    string
		tool    string
		want    string
		wantErr bool
	}{
		{"text and skipped image", "echo", "Echo: hi\n[image content omitted]", false},
		{"tool reports failure", "fail", "it broke", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &mcpTool{client: c, name: tt.tool, info: mcpToolInfo{Name: tt.tool}}
			out, err := tool.Run(context.Background(), map[string]any{"text": "hi"})
			if tt.wantErr {
				if err == nil || err.Error() != tt.want {
					t.Fatalf("err = %v, want %q", err, tt.want)
				}
				return
			}
			if err != nil || out != tt.want {
				t.Fatalf("out = %q, err = %v, want %q", out, err, tt.want)
			}
		})
	}
}

func TestMCPToolNeedsConfirmation(t *testing.T) {
	var tool Tool = &mcpTool{name: "fs__write"}
	if _, ok := tool.(Confirmable); !ok {
		t.Fatal("MCP tools must ask for confirmation")
	}
}

func TestMCPCallTimeout(t *testing.T) {
	c := startFake(t)
	c.timeout = 100 * time.Millisecond
	c.closeTimeout = 100 * time.Millisecond // the "hang" server won't exit on its own
	_, err := c.call(context.Background(), "hang", nil)
	if err == nil || !strings.Contains(err.Error(), "no reply") {
		t.Fatalf("err = %v, want timeout", err)
	}
}

func TestMCPCallCancel(t *testing.T) {
	c := startFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	_, err := c.call(ctx, "slow", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("took %s: the call should return when cancelled, not when the server replies", elapsed)
	}

	// The connection must still work. The server's late reply to the cancelled
	// request arrives first and has to be dropped, not mistaken for this one.
	result, err := c.call(context.Background(), "cancelled_ids", nil)
	if err != nil {
		t.Fatalf("a later call failed: %v", err)
	}
	if strings.Contains(string(result), "late") {
		t.Fatalf("got the cancelled request's reply: %s", result)
	}
	// And the server was told to stop (the "slow" request had id 1).
	if !strings.Contains(string(result), "[1]") {
		t.Fatalf("server was not sent notifications/cancelled: %s", result)
	}
}

func TestMCPServerExitsMidCall(t *testing.T) {
	c := startFake(t)
	_, err := c.call(context.Background(), "die", nil)
	if err == nil || !strings.Contains(err.Error(), "closed its output") {
		t.Fatalf("err = %v, want the server-closed error", err)
	}
	// Later calls fail at once instead of waiting for the timeout.
	start := time.Now()
	if _, err := c.call(context.Background(), "tools/list", nil); err == nil || time.Since(start) > time.Second {
		t.Fatalf("a call to a dead server should fail fast, got %v after %s", err, time.Since(start))
	}
}

func TestMCPConcurrentCalls(t *testing.T) {
	c := startFake(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.call(context.Background(), "tools/list", nil); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestMCPCloseKillsStubbornServer(t *testing.T) {
	t.Setenv("GO_FAKE_MCP_SERVER", "stubborn")
	c, err := StartMCP(os.Args[0], "-test.run=TestHelperProcess")
	if err != nil {
		t.Fatal(err)
	}
	c.closeTimeout = 200 * time.Millisecond

	start := time.Now()
	c.Close()
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Close took %s; it should kill a server that won't exit", elapsed)
	}
}
