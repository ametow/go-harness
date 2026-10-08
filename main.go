package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	model := flag.String("model", "llama3.2", "Ollama model name")
	url := flag.String("url", "http://localhost:11434", "Ollama base URL")
	flag.Parse()

	client := &Client{BaseURL: *url, Model: *model}

	reply, err := client.Chat([]Message{{Role: "user", Content: "Say hello in one short sentence."}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(reply.Content)
}
