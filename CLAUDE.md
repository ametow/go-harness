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

## Roadmap

Built one step at a time, in order. Each step is reviewed and committed before the next starts. Update the checkboxes as steps land.

- [x] 1. **LLM client:** `Client.Chat` POSTs to Ollama's `/api/chat` and returns the reply. (`llm.go`)
- [x] 2. **Chat loop:** read a line from stdin, keep the history, print the answer. (`main.go`)
- [x] 3. **Tool calling:** a `Tool` interface, `current_time` and `read_file` tools, tool definitions sent to the model, tool calls parsed and run. (`tools.go`)
- [x] 4. **Agent loop:** after running tools, feed the results back to the model and repeat until it replies with plain text. Add a max-iterations guard.
- [x] 5. **More tools and safety:** a few more useful tools (e.g. list directory, write file, run command), with limits such as a working-directory sandbox, output size caps and confirmation before anything destructive.
- [x] 6. **System prompt and config:** a system prompt, plus settings (model, endpoint, limits) from flags or env vars.
- [x] 7. **Streaming:** print the reply token by token instead of waiting for the full response.
- [x] 8. **MCP servers:** act as an MCP client, speaking JSON-RPC over stdio to external servers, discovering their tools and exposing them like built-in ones. Hand-rolled, no SDK. Built in three parts:
  - [x] 8a. JSON-RPC over stdio and the `initialize` handshake. (`mcp.go`)
  - [x] 8b. Tool discovery: `tools/list`, wrapping each server tool as a `Tool`. (`mcp.go`)
  - [x] 8c. Config file to choose servers, `tools/call`, and the confirmation gate for MCP tools. (`mcpconfig.go`)
- [ ] 9. **Persistent memory:** Built in two parts:
  - [x] 9a. Save the conversation to a JSON file after every turn and resume it on start. (`session.go`)
  - [ ] 9b. Long-term facts: `remember` and `recall` tools backed by a JSON file, so the agent keeps notes across sessions and `-new` chats.
- [ ] 10. **Context management:** keep the history within the model's context window by trimming or summarizing old turns.
- [ ] 11. **Model routing:** pick a model per task (e.g. a small fast model for simple turns, a larger one for tool use or hard problems), with a simple rule-based router first.
- [ ] 12. **Polish:** tests for the pure logic, error handling and logging, and a README.

The order can change as we learn. Ask the owner before reordering or adding steps.

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
- Settings come from flags, then env vars, then defaults (`config.go`): `-model`/`HARNESS_MODEL`, `-url`/`HARNESS_URL`, `-system`/`HARNESS_SYSTEM`, `-max-steps`/`HARNESS_MAX_STEPS`, `-mcp-config`/`HARNESS_MCP_CONFIG`, `-session`/`HARNESS_SESSION`, and `-new` (ignore the saved conversation).
- Ollama must be running (`ollama serve`) with a tool-capable model pulled. Default is `qwen3.5:9b`; `llama3.2` (3B) calls tools indiscriminately and is not recommended.

## MCP servers

External tools come from MCP servers listed in `mcp.json` (path set by `-mcp-config`; a missing default file just means no servers):

```json
{"mcpServers": {"fs": {"command": "npx", "args": ["-y", "some-mcp-server", "/path"]}}}
```

The repo's sample `mcp.json` runs `examples/adder.py`, a tiny Python server with one `add` tool, so MCP works with no downloads.

Each tool is exposed to the model as `<server>__<tool>`. Every MCP tool call asks for y/N confirmation. A server that fails to start is skipped with a warning.
