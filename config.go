package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

// defaultMCPConfig is looked for in the current directory; it's fine if it's absent.
const defaultMCPConfig = "mcp.json"

// defaultSession is where the conversation is saved, in the current directory.
const defaultSession = "session.json"

const defaultSystemPrompt = `You are a helpful assistant running in a terminal on the user's computer.
Answer directly and briefly in plain text.
You have tools. Use one only when the request needs it: for example, the current time, a file's contents, a directory listing, or running a command.
Do not use tools for greetings or general questions.
If a tool returns an error, read it and try a different approach, or tell the user what went wrong.`

// Config holds the settings that change between runs.
type Config struct {
	Model     string
	URL       string
	System    string
	MaxSteps  int
	MCPConfig string
	Session   string // file the conversation is saved to and resumed from
	NewChat   bool   // ignore any saved conversation and start fresh
}

// parseConfig builds a Config from args. Precedence: flag > environment
// variable > built-in default. It takes args (rather than reading os.Args)
// so it can be tested.
func parseConfig(args []string) (Config, error) {
	steps, err := envInt("HARNESS_MAX_STEPS", 10)
	if err != nil {
		return Config{}, err
	}

	var c Config
	fs := flag.NewFlagSet("go-harness", flag.ContinueOnError)
	fs.StringVar(&c.Model, "model", envOr("HARNESS_MODEL", "qwen3.5:9b"), "Ollama model name (env HARNESS_MODEL)")
	fs.StringVar(&c.URL, "url", envOr("HARNESS_URL", "http://localhost:11434"), "Ollama base URL (env HARNESS_URL)")
	fs.StringVar(&c.System, "system", envOr("HARNESS_SYSTEM", defaultSystemPrompt), "system prompt (env HARNESS_SYSTEM)")
	fs.IntVar(&c.MaxSteps, "max-steps", steps, "max model calls per message (env HARNESS_MAX_STEPS)")
	fs.StringVar(&c.MCPConfig, "mcp-config", envOr("HARNESS_MCP_CONFIG", defaultMCPConfig), "MCP servers config file (env HARNESS_MCP_CONFIG)")
	fs.StringVar(&c.Session, "session", envOr("HARNESS_SESSION", defaultSession), "conversation file to save and resume (env HARNESS_SESSION)")
	fs.BoolVar(&c.NewChat, "new", false, "start a fresh conversation instead of resuming the saved one")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if c.MaxSteps < 1 {
		return Config{}, fmt.Errorf("max-steps must be at least 1, got %d", c.MaxSteps)
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number, got %q", key, v)
	}
	return n, nil
}
