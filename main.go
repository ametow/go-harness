package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	model := flag.String("model", "qwen2.5:7b", "Ollama model name")
	url := flag.String("url", "http://localhost:11434", "Ollama base URL")
	flag.Parse()

	client := &Client{BaseURL: *url, Model: *model}

	// The model is stateless: it only "remembers" what we resend each turn,
	// so we keep the whole conversation and grow it as we go.
	var history []Message

	tools := []Tool{CurrentTime{}, ReadFile{}}

	fmt.Printf("Chatting with %s. Type 'exit' or press Ctrl-D to quit.\n", *model)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break // EOF (Ctrl-D) or read error
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" {
			break
		}

		history = append(history, Message{Role: "user", Content: line})

		reply, err := client.Chat(history, tools)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			// Drop the unanswered user message so a retry doesn't send it twice.
			history = history[:len(history)-1]
			continue
		}

		history = append(history, reply)
		if reply.Content != "" {
			fmt.Println(reply.Content)
		}

		// Step 3: run any requested tools and record the results. We don't
		// send them back to the model yet; that loop is step 4.
		for _, call := range reply.ToolCalls {
			name := call.Function.Name
			fmt.Printf("[tool call] %s %v\n", name, call.Function.Arguments)

			result := runTool(tools, call)
			fmt.Printf("[tool result] %s\n", result)
			history = append(history, Message{Role: "tool", Content: result, ToolName: name})
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "read error:", err)
		os.Exit(1)
	}
}

// runTool executes one tool call. Failures are returned as text rather than
// aborting, so that later the model can see the error and recover.
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
