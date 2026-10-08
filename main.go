package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	model := flag.String("model", "llama3.2", "Ollama model name")
	url := flag.String("url", "http://localhost:11434", "Ollama base URL")
	flag.Parse()

	client := &Client{BaseURL: *url, Model: *model}

	// The model is stateless: it only "remembers" what we resend each turn,
	// so we keep the whole conversation and grow it as we go.
	var history []Message

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

		reply, err := client.Chat(history)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			// Drop the unanswered user message so a retry doesn't send it twice.
			history = history[:len(history)-1]
			continue
		}

		history = append(history, reply)
		fmt.Println(reply.Content)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "read error:", err)
		os.Exit(1)
	}
}
