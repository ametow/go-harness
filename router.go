package main

import "strings"

// Router decides which model handles a turn. Small models are quick but poor
// at calling tools (we saw llama3.2 call tools for "hi"), so the rule is:
//
//   - chit-chat goes to the Fast model, and tools are NOT offered to it;
//   - anything that might need a tool, or just looks substantial, goes to the
//     Main model with all tools.
//
// When in doubt it picks Main. A wrong "fast" pick gives a worse answer; a
// wrong "main" pick only costs some speed.
type Router struct {
	Main string
	Fast string // empty disables routing: everything goes to Main
}

// Words that suggest the request needs a tool or the user's saved information.
// Matching is by substring on lowercase text, so it errs towards Main
// ("sometimes" matches "time"), which is the safe direction.
var needsMainWords = []string{
	"file", "read", "run", "list", "director", "folder", "command", "bash", "shell",
	"time", "date", "today", "now", "write", "open", "search", "find", "check", "look",
	"remember", "recall", "remind", "note", "save", "memory", "forget",
	"my ", " i ", "i'm", "told", "earlier", "before", "again", "previous", "last",
	"code", "script", "```", "/", "add", "sum", "calculate",
	"tool", "mcp", "server", "capabilit", "available", "function",
	"fix", "debug", "error", "bug", "explain", "implement", "detail",
	".go", ".md", ".json", ".txt", ".py", ".yaml", // file names
}

// maxSimpleWords: longer messages are assumed to be substantial.
const maxSimpleWords = 8

// Route returns the model to use for a user message, and whether tools should
// be offered to it.
func (r Router) Route(userMessage string) (model string, withTools bool) {
	if r.Fast == "" || !looksSimple(userMessage) {
		return r.Main, true
	}
	return r.Fast, false
}

// maxStatementWords: a message that is not a question must be this short to
// count as simple ("thanks, that was great!"). Longer statements may be news
// the agent should save ("the secret word is pineapple"), which needs a tool.
const maxStatementWords = 4

func looksSimple(msg string) bool {
	msg = strings.TrimSpace(msg)
	// Punctuation becomes spaces so " i " matches in "who am i?".
	// Dots stay: they are part of file names like main.go.
	text := " " + strings.NewReplacer("?", " ", "!", " ", ",", " ", ";", " ", ":", " ").Replace(strings.ToLower(msg)) + " "

	words := len(strings.Fields(text))
	if words > maxSimpleWords || (!strings.HasSuffix(msg, "?") && words > maxStatementWords) {
		return false
	}
	for _, w := range needsMainWords {
		if strings.Contains(text, w) {
			return false
		}
	}
	return true
}
