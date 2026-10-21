# go-harness

A small AI agent harness written in Go with only the standard library. It runs a loop that talks to a local LLM through [Ollama](https://ollama.com), lets the model use tools, and keeps its memory on disk. About 1,500 lines, built step by step as a way to learn how agents work.

## Quick start

You need Go and a running Ollama with a model that supports tool calling:

```bash
ollama pull qwen3.5:9b
go run .
```

Type a message and press Enter. Type `exit` or press Ctrl-D to quit. Try:

- `What time is it?` — the model calls the `current_time` tool.
- `Read go.mod and tell me the Go version.`
- `My dog is called Biscuit.` — saved to long-term memory, and remembered in later chats.

## How it works

```
you ──► history ──► model ──► plain text? ──► print, done
                      ▲            │ no: it wants tools
                      │            ▼
                      └──── run tools, add results to history
```

The model only emits a tool name and arguments; this program runs the tool and sends the result back. The loop repeats until the model answers in plain text, or stops after `-max-steps` calls.

| File | What it does |
|---|---|
| `main.go` | Reads input, wires everything together, handles Ctrl-C |
| `agent.go` | The loop: call the model, run tools, repeat |
| `llm.go` | Streaming client for Ollama's `/api/chat` |
| `tools.go` | The `Tool` interface and built-in tools |
| `mcp.go`, `mcpconfig.go` | MCP client: use tools from external servers |
| `memory.go` | `remember` and `recall` tools (long-term notes) |
| `session.go` | Saves and resumes the conversation |
| `context.go` | Keeps the history within the context window (trim, summarize) |
| `router.go` | Sends simple chat to a small fast model |
| `config.go` | Flags and environment variables |

## Settings

Each setting is a flag, with an environment variable as the fallback, then a default.

| Flag | Env var | Default | Meaning |
|---|---|---|---|
| `-model` | `HARNESS_MODEL` | `qwen3.5:9b` | Main model |
| `-fast-model` | `HARNESS_FAST_MODEL` | none | Small model for simple chat; empty turns routing off |
| `-url` | `HARNESS_URL` | `http://localhost:11434` | Ollama address |
| `-system` | `HARNESS_SYSTEM` | built-in prompt | System prompt |
| `-max-steps` | `HARNESS_MAX_STEPS` | `10` | Max model calls per message |
| `-context-tokens` | `HARNESS_CONTEXT_TOKENS` | `4000` | History size sent per call (estimated tokens); `0` = no limit |
| `-summarize` | `HARNESS_SUMMARIZE` | `true` | Summarize old turns instead of dropping them |
| `-session` | `HARNESS_SESSION` | `session.json` | Conversation file |
| `-new` | | | Start fresh instead of resuming the session |
| `-memory` | `HARNESS_MEMORY` | `memory.json` | Long-term notes file |
| `-mcp-config` | `HARNESS_MCP_CONFIG` | `mcp.json` | MCP servers file |
| `-verbose` | `HARNESS_VERBOSE` | `false` | Log each model call (tokens, time) to stderr |

Keep `-context-tokens` below the model's context size in Ollama (`num_ctx`), which Ollama silently enforces by cutting off the start of the prompt. `-verbose` shows how many tokens the model really read, which is a good way to tune it.

## Tools

Built in: `bash`, `read_file`, `list_directory`, `current_time`, `remember`, `recall`.

`bash` and every MCP tool ask `[y/N]` before running. Output is capped at 8000 bytes, and bash is killed after 15 seconds. There is no sandbox: an approved `bash` command can do anything you can.

### Adding a tool

Implement the `Tool` interface in `tools.go` and add it to the list in `main.go`:

```go
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any // JSON Schema for the arguments
	Run(args map[string]any) (string, error)
}
```

Also implement `NeedsConfirmation(args) string` if the tool changes anything, and the user will be asked first. Errors you return are shown to the model, so it can recover.

## MCP servers

[MCP](https://modelcontextprotocol.io) servers add tools from outside programs. List them in `mcp.json`:

```json
{"mcpServers": {"math": {"command": "python3", "args": ["examples/adder.py"]}}}
```

The sample file runs a tiny Python server with one `add` tool, so it works with no downloads. Tools appear to the model as `<server>__<tool>`, e.g. `math__add`. A server that fails to start is skipped with a warning.

## Memory

- **Session** (`session.json`): the whole conversation, saved after every turn and resumed on start. `-new` starts fresh.
- **Notes** (`memory.json`): short facts the model saves with `remember`. They survive `-new`. Delete or edit the file to forget.
- **Context window**: when the history outgrows `-context-tokens`, the oldest turns stop being sent and are folded into a short summary. The session file still keeps everything.

## Model routing

With `-fast-model` set, short chit-chat goes to the small model with no tools offered (small models call tools badly); anything that might need a tool, or looks substantial, goes to the main model. When in doubt it picks the main model. The rules are plain keywords in `router.go`.

## Development

```bash
go run .          # run
go test ./...     # tests; none need a live model
go vet ./...
```

The tests use fake Ollama and MCP servers, so they run offline. See `CLAUDE.md` for the project's principles and the step-by-step roadmap it was built from.

## Known limits

- The router is keyword-based, so a short message that secretly needs a tool can go to the small model.
- Token counts are estimates (about 4 characters per token).
- Summaries are kept in memory only and rebuilt after a resume.
- MCP: tools only. Images in tool results are skipped, and requests from the server to the client are ignored.
- Unix-first: Ctrl-C handling for MCP servers uses process groups.
