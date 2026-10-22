package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Message is one turn in a conversation. Role is "system", "user" or "assistant".
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"` // set on assistant replies that want a tool run
	ToolName  string     `json:"tool_name,omitempty"`  // set on role "tool" messages carrying a result
}

// ToolCall is the model asking us to run a tool.
type ToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

// toolDef is how a Tool is described to Ollama.
type toolDef struct {
	Type     string `json:"type"` // always "function"
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

func newToolDef(t Tool) toolDef {
	d := toolDef{Type: "function"}
	d.Function.Name = t.Name()
	d.Function.Description = t.Description()
	d.Function.Parameters = t.Parameters()
	return d
}

// Client talks to Ollama's HTTP API.
type Client struct {
	BaseURL string // e.g. http://localhost:11434
	Model   string // e.g. llama3.2

	// Logf, if set, receives one line per model call (size, tokens, time)
	// when LogLast is called.
	Logf func(format string, args ...any)
	last string // description of the most recent Chat call
}

// LogLast logs the most recent Chat call. It is separate from Chat so callers
// can log after they have finished printing the streamed reply; otherwise the
// log line would land in the middle of it.
func (c *Client) LogLast() {
	if c.Logf != nil && c.last != "" {
		c.Logf("%s", c.last)
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []toolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream"` // true: reply arrives as a series of small JSON chunks
}

// chunkMessage is a Message plus the "thinking" text that reasoning models
// (like qwen3.5) send separately from their answer. Embedding Message keeps
// its fields (role, content, tool_calls) in the same JSON object.
type chunkMessage struct {
	Message
	Thinking string `json:"thinking"`
}

// chatChunk is one line of the streamed response. Content holds a few tokens
// of text; the last chunk has Done set.
type chatChunk struct {
	Message chunkMessage `json:"message"`
	Done    bool         `json:"done"`
	Error   string       `json:"error"`

	// Only on the final chunk: how many tokens the model really read and wrote.
	PromptTokens int `json:"prompt_eval_count"`
	ReplyTokens  int `json:"eval_count"`
}

// Chat sends the conversation to the model and returns its full reply. Cancelling
// ctx stops the request and the stream at once and returns ctx.Err(). The
// reply is streamed: onToken (if not nil) is called with each piece of text as
// it arrives, so the caller can show it live. If tools are given, the reply
// may contain ToolCalls instead of (or alongside) text.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool, onToken func(string)) (Message, error) {
	req := chatRequest{Model: c.Model, Messages: messages, Stream: true}
	for _, t := range tools {
		req.Tools = append(req.Tools, newToolDef(t))
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, fmt.Errorf("encode request: %w", err)
	}

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return Message{}, ctx.Err() // cancelled: say so, not "is it running?"
		}
		return Message{}, fmt.Errorf("call ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	// The body is a stream of JSON objects, one per chunk. Decoder reads them
	// one at a time. We stitch the chunks into a single message.
	reply := Message{Role: "assistant"}
	var thinking string
	dec := json.NewDecoder(resp.Body)
	for {
		var chunk chatChunk
		if err := dec.Decode(&chunk); err != nil {
			if ctx.Err() != nil {
				return Message{}, ctx.Err() // the stream was cut because we cancelled it
			}
			return Message{}, fmt.Errorf("decode response (status %s): %w", resp.Status, err)
		}
		if chunk.Error != "" {
			return Message{}, fmt.Errorf("ollama error: %s", chunk.Error)
		}

		reply.Content += chunk.Message.Content
		reply.ToolCalls = append(reply.ToolCalls, chunk.Message.ToolCalls...)
		thinking += chunk.Message.Thinking
		if onToken != nil && chunk.Message.Content != "" {
			onToken(chunk.Message.Content)
		}

		if chunk.Done {
			// A reasoning model sometimes writes its whole answer into the
			// thinking field and leaves the content empty. Ollama splits the two
			// by looking for the model's think tags, and the model doesn't always
			// close them. Without this we would throw a good answer away.
			if reply.Content == "" && len(reply.ToolCalls) == 0 && strings.TrimSpace(thinking) != "" {
				reply.Content = strings.TrimSpace(thinking)
				if onToken != nil {
					onToken(reply.Content)
				}
			}
			c.last = fmt.Sprintf("%s: sent %d messages and %d tools; model read %d tokens, wrote %d, took %s",
				c.Model, len(messages), len(tools), chunk.PromptTokens, chunk.ReplyTokens, time.Since(start).Round(time.Millisecond))
			return reply, nil
		}
	}
}
