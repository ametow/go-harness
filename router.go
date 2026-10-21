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

func looksSimple(msg string) bool {
	text := " " + strings.ToLower(strings.TrimSpace(msg)) + " "
	if len(strings.Fields(text)) > maxSimpleWords {
		return false
	}
	for _, w := range needsMainWords {
		if strings.Contains(text, w) {
			return false
		}
	}
	return true
}
