package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// serve returns a fake Ollama that replies with the given streamed body.
func serve(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Model: "test"}
}

func TestChatStreamsAndJoinsChunks(t *testing.T) {
	c := serve(t, `{"message":{"role":"assistant","content":"Hel"},"done":false}
{"message":{"role":"assistant","content":"lo"},"done":false}
{"message":{"role":"assistant","content":"!"},"done":true}
`)
	var tokens []string
	reply, err := c.Chat(nil, nil, func(s string) { tokens = append(tokens, s) })
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "Hello!" {
		t.Fatalf("content = %q, want %q", reply.Content, "Hello!")
	}
	if want := []string{"Hel", "lo", "!"}; !reflect.DeepEqual(tokens, want) {
		t.Fatalf("tokens = %q, want %q", tokens, want)
	}
}

func TestChatCollectsToolCalls(t *testing.T) {
	c := serve(t, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"current_time","arguments":{}}}]},"done":false}
{"message":{"role":"assistant","content":""},"done":true}
`)
	reply, err := c.Chat(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Function.Name != "current_time" {
		t.Fatalf("tool calls = %+v", reply.ToolCalls)
	}
}

func TestChatErrors(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"error line", `{"error":"model not found"}`, "model not found"},
		{"cut off mid-stream", `{"message":{"content":"Hi"},"done":false}`, "decode response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := serve(t, tt.body).Chat(nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
