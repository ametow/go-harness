package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scriptedOllama replies to each request with the next body in `replies`
// (repeating the last one), and records every request it receives.
func scriptedOllama(t *testing.T, replies ...string) (*Client, *[]chatRequest) {
	t.Helper()
	var requests []chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		i := min(len(requests), len(replies)-1)
		requests = append(requests, req)
		w.Write([]byte(replies[i] + "\n"))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Model: "test"}, &requests
}

const (
	toolCallReply = `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"current_time","arguments":{}}}]},"done":true}`
	finalReply    = `{"message":{"role":"assistant","content":"It is noon."},"done":true}`
)

func newTestAgent(c *Client, maxSteps int) *Agent {
	return &Agent{Client: c, Router: Router{Main: "test"}, Tools: []Tool{CurrentTime{}}, MaxSteps: maxSteps}
}

func start(msg string) []Message {
	return []Message{{Role: "system", Content: "s"}, {Role: "user", Content: msg}}
}

func TestRunTurnToolRoundTrip(t *testing.T) {
	client, requests := scriptedOllama(t, toolCallReply, finalReply)

	history, err := newTestAgent(client, 5).RunTurn(start("what time is it"))
	if err != nil {
		t.Fatal(err)
	}

	// system, user, assistant(tool call), tool result, assistant(final)
	if len(history) != 5 {
		t.Fatalf("history has %d messages, want 5: %+v", len(history), history)
	}
	if len(history[2].ToolCalls) != 1 || history[3].Role != "tool" || history[3].ToolName != "current_time" {
		t.Fatalf("tool call/result not recorded: %+v", history[2:4])
	}
	if history[4].Content != "It is noon." {
		t.Fatalf("final reply = %q", history[4].Content)
	}

	// The second request must carry the tool result back to the model.
	if len(*requests) != 2 {
		t.Fatalf("model called %d times, want 2", len(*requests))
	}
	second := (*requests)[1].Messages
	if last := second[len(second)-1]; last.Role != "tool" || last.Content == "" {
		t.Fatalf("second request should end with the tool result, got %+v", last)
	}
}

func TestRunTurnStopsAfterMaxSteps(t *testing.T) {
	client, requests := scriptedOllama(t, toolCallReply) // asks for a tool forever

	_, err := newTestAgent(client, 3).RunTurn(start("loop"))
	if err == nil || !strings.Contains(err.Error(), "gave up after 3 steps") {
		t.Fatalf("err = %v, want give-up error", err)
	}
	if len(*requests) != 3 {
		t.Fatalf("model called %d times, want exactly MaxSteps (3)", len(*requests))
	}
}

func TestRunToolErrorsAreReturnedAsText(t *testing.T) {
	call := ToolCall{}
	call.Function.Name = "no_such_tool"
	if got := runTool(nil, call); !strings.Contains(got, `unknown tool "no_such_tool"`) {
		t.Fatalf("got %q", got)
	}

	call.Function.Name = "read_file" // exists, but the argument is missing
	if got := runTool([]Tool{ReadFile{}}, call); !strings.HasPrefix(got, "error: ") {
		t.Fatalf("got %q, want an error string", got)
	}
}

// dangerous is a tool that must be confirmed; it records whether it ran.
type dangerous struct{ ran *bool }

func (dangerous) Name() string                            { return "danger" }
func (dangerous) Description() string                     { return "" }
func (dangerous) Parameters() map[string]any              { return map[string]any{"type": "object"} }
func (dangerous) NeedsConfirmation(map[string]any) string { return "do something risky" }
func (d dangerous) Run(map[string]any) (string, error)    { *d.ran = true; return "done", nil }

func TestRunToolConfirmation(t *testing.T) {
	tests := []struct {
		answer  string
		wantRun bool
	}{
		{"y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"\n", false}, // just Enter: default is no
		{"maybe\n", false},
		{"", false}, // EOF: no
	}
	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.answer)+"|", func(t *testing.T) {
			old := input
			input = bufio.NewReader(strings.NewReader(tt.answer))
			defer func() { input = old }()

			ran := false
			call := ToolCall{}
			call.Function.Name = "danger"
			got := runTool([]Tool{dangerous{&ran}}, call)

			if ran != tt.wantRun {
				t.Fatalf("tool ran = %v, want %v (result %q)", ran, tt.wantRun, got)
			}
			if !tt.wantRun && !strings.Contains(got, "declined") {
				t.Fatalf("a refusal should tell the model, got %q", got)
			}
		})
	}
}
