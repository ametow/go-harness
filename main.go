package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// input is shared so the chat prompt and tool confirmations read from the
// same buffered stream; two separate readers would swallow each other's input.
var input = bufio.NewReader(os.Stdin)

func main() {
	model := flag.String("model", "qwen3.5:9b", "Ollama model name")
	url := flag.String("url", "http://localhost:11434", "Ollama base URL")
	flag.Parse()

	client := &Client{BaseURL: *url, Model: *model}

	// The model is stateless: it only "remembers" what we resend each turn,
	// so we keep the whole conversation and grow it as we go.
	var history []Message

	tools := []Tool{Bash{}, ListDirectoryFiles{}, CurrentTime{}, ReadFile{}}

	fmt.Printf("Chatting with %s. Type 'exit' or press Ctrl-D to quit.\n", *model)

	for {
		fmt.Print("> ")
		raw, err := input.ReadString('\n')
		line := strings.TrimSpace(raw)
		if err != nil && line == "" {
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, "read error:", err)
				os.Exit(1)
			}
			break // Ctrl-D
		}
		if line == "" {
			continue
		}
		if line == "exit" {
			break
		}

		before := len(history)
		history = append(history, Message{Role: "user", Content: line})

		history, err = runTurn(client, history, tools)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			// Roll back the whole failed turn so a retry starts clean.
			history = history[:before]
		}
	}
}
