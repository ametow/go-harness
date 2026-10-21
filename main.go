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
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		if err != flag.ErrHelp {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(2)
	}

	client := &Client{BaseURL: cfg.URL, Model: cfg.Model}

	// The model is stateless: it only "remembers" what we resend each turn,
	// so we keep the whole conversation and grow it as we go.
	// The system message goes first and is never trimmed or rolled back.
	history := []Message{{Role: "system", Content: cfg.System}}

	tools := []Tool{Bash{}, ListDirectoryFiles{}, CurrentTime{}, ReadFile{}, Remember{cfg.Memory}, Recall{cfg.Memory}}

	// Resume the saved conversation, unless -new. The system message is never
	// saved: it always comes from the current config, so changing -system takes effect.
	if !cfg.NewChat {
		saved, err := loadSession(cfg.Session)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		if len(saved) > 0 {
			history = append(history, saved...)
			fmt.Printf("Resumed %d messages from %s (use -new to start fresh).\n", len(saved), cfg.Session)
		}
	}

	// Tools from external MCP servers join the built-in ones.
	servers, err := loadMCPConfig(cfg.MCPConfig, cfg.MCPConfig == defaultMCPConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	mcpTools, mcpClients := connectMCPServers(servers)
	defer closeMCP(mcpClients)
	tools = append(tools, mcpTools...)

	fmt.Printf("Chatting with %s. Type 'exit' or press Ctrl-D to quit.\n", cfg.Model)

	for {
		fmt.Print("> ")
		raw, err := input.ReadString('\n')
		line := strings.TrimSpace(raw)
		if err != nil && line == "" {
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, "read error:", err)
				closeMCP(mcpClients) // os.Exit skips deferred calls
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

		history, err = runTurn(client, history, tools, cfg.MaxSteps, cfg.ContextTokens)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			// Roll back the whole failed turn so a retry starts clean.
			history = history[:before]
			continue
		}
		if err := saveSession(cfg.Session, history[1:]); err != nil { // [1:] skips the system message
			fmt.Fprintln(os.Stderr, "warning: could not save session:", err)
		}
	}
}

func closeMCP(clients []*MCPClient) {
	for _, c := range clients {
		c.Close()
	}
}
