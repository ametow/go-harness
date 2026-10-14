package main

import "fmt"

// maxSteps caps model calls per user message, so a model that keeps asking
// for tools can't loop forever.
const maxSteps = 10

// runTurn handles one user message: call the model, run any tools it asks
// for, send the results back, and repeat until it answers in plain text.
// It returns the history extended with everything that happened.
func runTurn(client *Client, history []Message, tools []Tool) ([]Message, error) {
	for step := 0; step < maxSteps; step++ {
		reply, err := client.Chat(history, tools)
		if err != nil {
			return history, err
		}
		history = append(history, reply)

		if reply.Content != "" {
			fmt.Println(reply.Content)
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
	return history, fmt.Errorf("gave up after %d steps without a final answer", maxSteps)
}

// runTool executes one tool call. Failures are returned as text rather than
// aborting, so the model can see the error and recover.
func runTool(tools []Tool, call ToolCall) string {
	tool, ok := findTool(tools, call.Function.Name)
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", call.Function.Name)
	}
	out, err := tool.Run(call.Function.Arguments)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}
