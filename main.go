package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	model := flag.String("model", "qwen3.5:9b", "Ollama model name")
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

		before := len(history)
		history = append(history, Message{Role: "user", Content: line})

		var err error
		history, err = runTurn(client, history, tools)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			// Roll back the whole failed turn so a retry starts clean.
			history = history[:before]
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "read error:", err)
		os.Exit(1)
	}
}
