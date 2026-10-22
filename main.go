package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	if cfg.Verbose {
		client.Logf = func(format string, args ...any) { fmt.Fprintf(os.Stderr, "[verbose] "+format+"\n", args...) }
	}

	// The model is stateless: it only "remembers" what we resend each turn,
	// so we keep the whole conversation and grow it as we go.
	// The system message goes first and is never trimmed or rolled back.
	history := []Message{{Role: "system", Content: cfg.System}}

	router := Router{Main: cfg.Model, Fast: cfg.FastModel}

	var summarizer *Summarizer // nil means old turns are simply dropped
	if cfg.Summarize {
		// Summarizing is an easy job, so it uses the fast model when there is one.
		summaryClient := *client
		if cfg.FastModel != "" {
			summaryClient.Model = cfg.FastModel
		}
		summarizer = &Summarizer{chat: func(ctx context.Context, msgs []Message) (Message, error) {
			reply, err := summaryClient.Chat(ctx, msgs, nil, nil)
			summaryClient.LogLast()
			return reply, err
		}}
	}

	tools := []Tool{Bash{}, ListDirectoryFiles{}, CurrentTime{}, ReadFile{}, Remember{cfg.Memory}, Recall{cfg.Memory}}

	// Resume the saved conversation, or with -new clear it. The system message
	// is never saved: it always comes from the current config, so changing
	// -system takes effect.
	saved, err := openSession(cfg.Session, cfg.NewChat, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	history = append(history, saved...)

	// Tools from external MCP servers join the built-in ones.
	servers, err := loadMCPConfig(cfg.MCPConfig, cfg.MCPConfig == defaultMCPConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	mcpTools, mcpClients := connectMCPServers(context.Background(), servers)
	defer closeMCP(mcpClients)

	// Ctrl-C cancels the running turn; at the prompt (or pressed twice) it
	// quits, shutting the MCP servers down cleanly first. (os.Exit skips
	// deferred calls, so the quit path has to close them itself.)
	turns := &turnControl{}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		for range interrupt {
			if !turns.interrupt() {
				fmt.Println("\n[cancelling. Press Ctrl-C again to quit]")
				continue
			}
			restoreTerminal() // if Ctrl-C came while a line was being edited
			fmt.Println()
			closeMCP(mcpClients)
			os.Exit(130) // the conventional exit status for "killed by Ctrl-C"
		}
	}()
	tools = append(tools, mcpTools...)

	agent := &Agent{Client: client, Router: router, Tools: tools, MaxSteps: cfg.MaxSteps, ContextTokens: cfg.ContextTokens, Summarizer: summarizer}

	state := &chatState{cfg: cfg, history: history, summarizer: summarizer, out: os.Stdout}

	fmt.Printf("Chatting with %s. Type /help for commands, or press Ctrl-D to quit.\n", cfg.Model)

	for {
		raw, err := readLine("> ", matchCommands, describeCommand)
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
		if isCommandLine(line) {
			if state.runCommand(line) {
				break
			}
			continue
		}

		before := len(state.history)
		state.history = append(state.history, Message{Role: "user", Content: line})

		ctx := turns.begin()
		state.history, err = agent.RunTurn(ctx, state.history)
		turns.end()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Println("\n[cancelled]")
			} else {
				fmt.Fprintln(os.Stderr, "error:", err)
			}
			// Roll back the whole failed turn so a retry starts clean.
			state.history = state.history[:before]
			continue
		}
		state.save()
	}
}

func closeMCP(clients []*MCPClient) {
	for _, c := range clients {
		c.Close()
	}
}
