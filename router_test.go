package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterRoute(t *testing.T) {
	r := Router{Main: "big", Fast: "small"}
	tests := []struct {
		msg       string
		wantModel string
		wantTools bool
	}{
		{"hi there", "small", false},
		{"Thanks, that was great!", "small", false},
		{"Tell me a joke", "small", false},
		{"What is the capital of France?", "small", false},
		{"What time is it?", "big", true},                                                           // needs current_time
		{"read go.mod", "big", true},                                                                // needs read_file
		{"What did I tell you about my dog?", "big", true},                                          // needs recall
		{"Explain in detail how goroutines and channels work together in Go programs", "big", true}, // long
		{"fix main.go", "big", true},                                                                // path-like
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			model, withTools := r.Route(tt.msg)
			if model != tt.wantModel || withTools != tt.wantTools {
				t.Fatalf("Route(%q) = %s, %v; want %s, %v", tt.msg, model, withTools, tt.wantModel, tt.wantTools)
			}
		})
	}
}

func TestRouterDisabledWithoutFastModel(t *testing.T) {
	model, withTools := Router{Main: "big"}.Route("hi")
	if model != "big" || !withTools {
		t.Fatalf("got %s, %v; routing should be off", model, withTools)
	}
}

// TestAgentUsesRoutedModel checks what is actually sent over the wire.
func TestAgentUsesRoutedModel(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done":true}` + "\n"))
	}))
	defer srv.Close()

	agent := &Agent{
		Client:   &Client{BaseURL: srv.URL, Model: "big"},
		Router:   Router{Main: "big", Fast: "small"},
		Tools:    []Tool{CurrentTime{}},
		MaxSteps: 3,
	}

	tests := []struct {
		msg       string
		wantModel string
		wantTools int
	}{
		{"hello", "small", 0},
		{"what time is it", "big", 1},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			got = chatRequest{}
			if _, err := agent.RunTurn([]Message{{Role: "system", Content: "s"}, {Role: "user", Content: tt.msg}}); err != nil {
				t.Fatal(err)
			}
			if got.Model != tt.wantModel || len(got.Tools) != tt.wantTools {
				t.Fatalf("sent model %q with %d tools; want %q with %d", got.Model, len(got.Tools), tt.wantModel, tt.wantTools)
			}
		})
	}
	if agent.Client.Model != "big" {
		t.Fatal("routing must not change the agent's main client")
	}
}
