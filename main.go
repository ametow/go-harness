package main

import (
	"bufio"
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
		summarizer = &Summarizer{chat: func(msgs []Message) (Message, error) {
			reply, err := summaryClient.Chat(msgs, nil, nil)
			summaryClient.LogLast()
			return reply, err
		}}
	}

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

	// On Ctrl-C, shut the MCP servers down cleanly before exiting. (os.Exit
	// skips deferred calls, so this has to close them itself.)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		restoreTerminal() // if Ctrl-C came while a line was being edited
		fmt.Println()
		closeMCP(mcpClients)
		os.Exit(130) // the conventional exit status for "killed by Ctrl-C"
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

		state.history, err = agent.RunTurn(state.history)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
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
