# go-harness

A thin agent harness in Go. It runs a loop that talks to a local LLM (e.g. Ollama) and performs simple tasks via tools.

This is a learning project. The owner reviews every step, so clarity beats cleverness.

## Principles

- **Start small.** Build one piece at a time. Each change should be small enough to read in a few minutes.
- **No big frameworks.** Standard library only unless there is a strong reason. No agent/LLM frameworks (LangChain-style); call the model's HTTP API directly with `net/http` and `encoding/json`.
- **Explain as you go.** When adding something new, say briefly what it does and why, so the owner can learn from it.
- **Don't build ahead.** No abstractions, config systems, or features that the current step doesn't need.
- **Ask before adding a dependency.**

## Architecture (target)

The core is a simple loop:

1. Send the conversation (messages + tool definitions) to the local LLM.
2. If the response contains tool calls, run them and append the results as messages.
3. Repeat until the model replies with plain text and no tool calls.

Planned pieces, to be added incrementally and in roughly this order:

1. LLM client: one function that POSTs to Ollama's `/api/chat` and returns the reply.
2. Chat loop: read a line from stdin, call the model, print the answer.
3. Tool calling: a small `Tool` interface plus one or two trivial tools (e.g. read a file, get the time).
4. The agent loop: run tools and feed results back until done.

## Conventions

- Go, latest stable version. Module name: `go-harness`.
- Format with `gofmt`; keep `go vet ./...` clean.
- Keep the package layout flat until it hurts. Start with `main.go`; split files only when one gets unwieldy.
- Handle errors explicitly and return them with context (`fmt.Errorf("...: %w", err)`).
- Comments explain *why*, not *what*.
- Tests: table-driven, standard `testing` package. Add them for logic that can be tested without a live model.

## Commands

```bash
go run .          # run the harness
go build ./...    # build
go vet ./...      # static checks
go test ./...     # run tests
```

## Local LLM

- Default endpoint: `http://localhost:11434` (Ollama).
- Model name is configured via a flag or env var, not hardcoded deep in the code.
- Ollama must be running (`ollama serve`) with a tool-capable model pulled.
