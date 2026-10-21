package main

import (
	"fmt"
	"os"
	"strings"
)

// Agent holds everything needed to answer a user message.
type Agent struct {
	Client        *Client // its Model is the main model
	Router        Router
	Tools         []Tool
	MaxSteps      int // cap on model calls per user message
	ContextTokens int
	Summarizer    *Summarizer
}

// RunTurn handles the latest user message in history: call the model, run any
// tools it asks for, send the results back, and repeat until it answers in
// plain text. MaxSteps stops a model that keeps asking for tools from looping
// forever. It returns the history extended with everything that happened.
func (a *Agent) RunTurn(history []Message) ([]Message, error) {
	// Pick the model once per turn, from the user's message. The choice holds
	// for every step, so a tool call never switches models half way.
	model, withTools := a.Router.Route(history[len(history)-1].Content)
	client := *a.Client // a copy, so the chosen model applies to this turn only
	client.Model = model
	tools := a.Tools
	if !withTools {
		tools = nil
	}
	if a.Router.Fast != "" {
		note := ""
		if !withTools {
			note = ", no tools"
		}
		fmt.Printf("[model: %s%s]\n", model, note)
	}

	for step := 0; step < a.MaxSteps; step++ {
		// Send a trimmed view; `history` itself stays complete for saving.
		view, dropped, err := a.Summarizer.View(history, a.ContextTokens)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v; dropping old messages without a summary\n", err)
		}
		if dropped > 0 && step == 0 {
			how := "not sending"
			if a.Summarizer != nil && err == nil {
				how = "summarizing"
			}
			fmt.Printf("[context full: %s the %d oldest messages]\n", how, dropped)
		}

		reply, err := client.Chat(view, tools, func(tok string) { fmt.Print(tok) })
		if err != nil {
			return history, err
		}
		history = append(history, reply)

		if reply.Content != "" {
			fmt.Println() // tokens were printed as they arrived; end the line
		}

		// No tool calls means the model is done and this is its final answer.
		if len(reply.ToolCalls) == 0 {
			return history, nil
		}

		for _, call := range reply.ToolCalls {
			name := call.Function.Name
			fmt.Printf("[tool call] %s %v\n", name, call.Function.Arguments)

			result := runTool(tools, call)
			fmt.Printf("[tool result] %s\n", result)
			history = append(history, Message{Role: "tool", Content: result, ToolName: name})
		}
		// Loop: the model now sees the results and decides what to do next.
	}
	return history, fmt.Errorf("gave up after %d steps without a final answer", a.MaxSteps)
}

// runTool executes one tool call. Failures are returned as text rather than
// aborting, so the model can see the error and recover.
func runTool(tools []Tool, call ToolCall) string {
	tool, ok := findTool(tools, call.Function.Name)
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", call.Function.Name)
	}
	// Tools that can change the machine need a human yes first. A refusal is
	// returned as text so the model knows and can pick another approach.
	if c, ok := tool.(Confirmable); ok && !confirm(c.NeedsConfirmation(call.Function.Arguments)) {
		return "error: the user declined this tool call; do not retry it"
	}
	out, err := tool.Run(call.Function.Arguments)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}

// confirm shows what is about to run and waits for y/yes on the shared input.
// Anything else, including EOF, counts as no: the safe default.
func confirm(description string) bool {
	fmt.Printf("Allow %s? [y/N] ", description)
	answer, _ := input.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
