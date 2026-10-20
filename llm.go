package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []toolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream"` // true: reply arrives as a series of small JSON chunks
}

// chatChunk is one line of the streamed response. Content holds a few tokens
// of text; the last chunk has Done set.
type chatChunk struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
	Error   string  `json:"error"`
}

// Chat sends the conversation to the model and returns its full reply. The
// reply is streamed: onToken (if not nil) is called with each piece of text as
// it arrives, so the caller can show it live. If tools are given, the reply
// may contain ToolCalls instead of (or alongside) text.
func (c *Client) Chat(messages []Message, tools []Tool, onToken func(string)) (Message, error) {
	req := chatRequest{Model: c.Model, Messages: messages, Stream: true}
	for _, t := range tools {
		req.Tools = append(req.Tools, newToolDef(t))
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, fmt.Errorf("encode request: %w", err)
	}

	resp, err := http.Post(c.BaseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("call ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	// The body is a stream of JSON objects, one per chunk. Decoder reads them
	// one at a time. We stitch the chunks into a single message.
	reply := Message{Role: "assistant"}
	dec := json.NewDecoder(resp.Body)
	for {
		var chunk chatChunk
		if err := dec.Decode(&chunk); err != nil {
			return Message{}, fmt.Errorf("decode response (status %s): %w", resp.Status, err)
		}
		if chunk.Error != "" {
			return Message{}, fmt.Errorf("ollama error: %s", chunk.Error)
		}

		reply.Content += chunk.Message.Content
		reply.ToolCalls = append(reply.ToolCalls, chunk.Message.ToolCalls...)
		if onToken != nil && chunk.Message.Content != "" {
			onToken(chunk.Message.Content)
		}

		if chunk.Done {
			return reply, nil
		}
	}
}
