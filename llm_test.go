package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
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
	reply, err := c.Chat(context.Background(), nil, nil, func(s string) { tokens = append(tokens, s) })
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
	reply, err := c.Chat(context.Background(), nil, nil, nil)
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
			_, err := serve(t, tt.body).Chat(context.Background(), nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestChatLogsTokenCounts(t *testing.T) {
	c := serve(t, `{"message":{"role":"assistant","content":"hi"},"done":true,"prompt_eval_count":123,"eval_count":45}`+"\n")
	var line string
	c.Logf = func(format string, args ...any) { line = fmt.Sprintf(format, args...) }

	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if line != "" {
		t.Fatal("Chat must not log by itself; callers log after printing the reply")
	}
	c.LogLast()
	for _, want := range []string{"test:", "1 messages", "read 123 tokens", "wrote 45"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q missing %q", line, want)
		}
	}
}

func TestChatUsesThinkingWhenContentIsEmpty(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantText   string
		wantTokens []string
	}{
		{
			name: "answer only in thinking",
			body: `{"message":{"role":"assistant","content":"","thinking":"Your name"},"done":false}
{"message":{"role":"assistant","content":"","thinking":" is Sam."},"done":false}
{"message":{"role":"assistant","content":""},"done":true}`,
			wantText:   "Your name is Sam.",
			wantTokens: []string{"Your name is Sam."}, // shown once, at the end
		},
		{
			name: "real content wins; thinking is left out",
			body: `{"message":{"role":"assistant","content":"","thinking":"let me work it out"},"done":false}
{"message":{"role":"assistant","content":"It is 4."},"done":true}`,
			wantText:   "It is 4.",
			wantTokens: []string{"It is 4."},
		},
		{
			name:     "a tool call is not an empty reply",
			body:     `{"message":{"role":"assistant","content":"","thinking":"I should call a tool","tool_calls":[{"function":{"name":"current_time","arguments":{}}}]},"done":true}`,
			wantText: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokens []string
			reply, err := serve(t, tt.body+"\n").Chat(context.Background(), nil, nil, func(s string) { tokens = append(tokens, s) })
			if err != nil {
				t.Fatal(err)
			}
			if reply.Content != tt.wantText || !reflect.DeepEqual(tokens, tt.wantTokens) {
				t.Fatalf("content = %q, tokens = %q; want %q, %q", reply.Content, tokens, tt.wantText, tt.wantTokens)
			}
		})
	}
}

func TestChatCancelStopsTheStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // the server only notices a hang-up once the body is read
		w.Write([]byte(`{"message":{"role":"assistant","content":"Once"},"done":false}` + "\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // then the "model" goes quiet until the client hangs up
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got []string
	c := &Client{BaseURL: srv.URL, Model: "test"}

	start := time.Now()
	_, err := c.Chat(ctx, nil, nil, func(s string) {
		got = append(got, s)
		cancel() // the user presses Ctrl-C after the first word
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled (not a decode error)", err)
	}
	if !reflect.DeepEqual(got, []string{"Once"}) {
		t.Fatalf("tokens before the cancel = %q", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Chat did not return promptly after the cancel")
	}
}

func TestChatCancelledBeforeTheCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := serve(t, `{"message":{"content":"x"},"done":true}`+"\n").Chat(ctx, nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled, not \"is it running?\"", err)
	}
}
