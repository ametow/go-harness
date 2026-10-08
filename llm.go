package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Message is one turn in a conversation. Role is "system", "user" or "assistant".
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client talks to Ollama's HTTP API.
type Client struct {
	BaseURL string // e.g. http://localhost:11434
	Model   string // e.g. llama3.2
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"` // false: get one complete JSON reply instead of chunks
}

type chatResponse struct {
	Message Message `json:"message"`
	Error   string  `json:"error"`
}

// Chat sends the conversation to the model and returns its reply.
func (c *Client) Chat(messages []Message) (Message, error) {
	body, err := json.Marshal(chatRequest{Model: c.Model, Messages: messages})
	if err != nil {
		return Message{}, fmt.Errorf("encode request: %w", err)
	}

	resp, err := http.Post(c.BaseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("call ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Message{}, fmt.Errorf("decode response (status %s): %w", resp.Status, err)
	}
	if out.Error != "" {
		return Message{}, fmt.Errorf("ollama error: %s", out.Error)
	}
	return out.Message, nil
}
