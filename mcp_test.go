package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestHelperProcess isn't a real test. When the env var is set, the test
// binary runs as a tiny fake MCP server, so we can test the client with no
// external dependencies. (This is the pattern os/exec's own tests use.)
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_FAKE_MCP_SERVER") != "1" {
		return
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cursor string `json:"cursor"`
			} `json:"params"`
		}
		json.Unmarshal(in.Bytes(), &req)
		if req.ID == nil {
			continue // notification: no reply
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
	info, err := c.Initialize()
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "fake" || info.Version != "9.9" {
		t.Fatalf("server info = %+v", info)
	}
}

func TestMCPServerError(t *testing.T) {
	c := startFake(t)
	_, err := c.call("bogus", nil)
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
	if _, err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	tools, err := c.ListTools()
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
	if _, err := echo.Run(nil); err == nil {
		t.Fatal("Run should not work until step 8c")
	}
}
