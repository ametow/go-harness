package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unicode/utf8"
)

// A tiny line editor, so typing "/" can show command suggestions and Tab can
// complete them. Normally the terminal edits the line for us and only hands
// the program a finished line. To see each key as it is typed we switch the
// terminal to "raw-ish" mode (no line buffering, no echo) while reading a
// line, and draw the text ourselves. There is no cursor movement inside the
// line: you can type, delete the last character, and press Tab.

const (
	clearLine = "\r\x1b[K" // back to column 0 and erase the line
	dim       = "\x1b[2m"
	reset     = "\x1b[0m"
)

// readLine shows prompt and returns what the user typed. On a terminal it
// offers command suggestions; with piped input (or if the terminal can't be
// switched) it just reads a plain line.
func readLine(prompt string, matches func(string) []string, describe func(string) string) (string, error) {
	restore, err := enterRawMode()
	if err != nil {
		fmt.Print(prompt)
		raw, err := input.ReadString('\n')
		return strings.TrimRight(raw, "\r\n"), err
	}
	defer restore()
	return editLine(input, os.Stdout, prompt, matches, describe)
}

// editLine reads keys from r and draws the line on w until Enter.
// matches lists the commands that start with a prefix; describe gives the
// help text for one command.
func editLine(r *bufio.Reader, w io.Writer, prompt string, matches func(string) []string, describe func(string) string) (string, error) {
	var buf []rune
	draw := func() { render(w, prompt, buf, hintFor(string(buf), matches, describe)) }
	finish := func() { fmt.Fprint(w, "\n\x1b[K") } // step onto the hint line and clear it; output continues there

	draw()
	for {
		ch, _, err := r.ReadRune()
		if err != nil {
			finish()
			return string(buf), err
		}
		switch {
		case ch == '\r' || ch == '\n':
			finish()
			return string(buf), nil
		case ch == 4: // Ctrl-D
			if len(buf) == 0 {
				finish()
				return "", io.EOF
			}
			continue
		case ch == 127 || ch == 8: // Backspace
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
			}
		case ch == 21: // Ctrl-U
			buf = buf[:0]
		case ch == '\t':
			if m := matches(string(buf)); len(m) > 0 && isCommandPrefix(string(buf)) {
				buf = []rune(commonPrefix(m))
			}
		case ch == 27: // arrow keys and friends arrive as ESC [ ... ; ignore them
			skipEscape(r)
			continue
		case ch < 32: // other control keys
			continue
		default:
			buf = append(buf, ch)
		}
		draw()
	}
}

// skipEscape discards the rest of an escape sequence like ESC [ A (up arrow).
func skipEscape(r *bufio.Reader) {
	if r.Buffered() == 0 {
		return // a lone Esc key press
	}
	if b, _ := r.ReadByte(); b != '[' {
		return
	}
	for r.Buffered() > 0 {
		if b, _ := r.ReadByte(); b >= 0x40 && b <= 0x7e { // final byte of the sequence
			return
		}
	}
}

// render draws two lines: the prompt with the text typed so far, and below it
// a dimmed hint line. It leaves the cursor at the end of the typed text.
func render(w io.Writer, prompt string, buf []rune, hint string) {
	col := utf8.RuneCountInString(prompt) + len(buf)
	fmt.Fprint(w, clearLine, prompt, string(buf), "\n\x1b[K")
	if hint != "" {
		fmt.Fprint(w, dim, hint, reset)
	}
	fmt.Fprint(w, "\x1b[A\r") // back up to the prompt line
	if col > 0 {
		fmt.Fprintf(w, "\x1b[%dC", col)
	}
}

// isCommandPrefix reports whether text looks like a command being typed:
// starts with "/" and has no space or second "/" yet.
func isCommandPrefix(text string) bool {
	return strings.HasPrefix(text, "/") && !strings.ContainsAny(text[1:], " /")
}

func hintFor(text string, matches func(string) []string, describe func(string) string) string {
	if !isCommandPrefix(text) {
		return ""
	}
	switch m := matches(text); len(m) {
	case 0:
		return "no such command"
	case 1:
		return m[0] + "  " + describe(m[0])
	default:
		return strings.Join(m, "  ")
	}
}

func commonPrefix(words []string) string {
	prefix := words[0]
	for _, w := range words[1:] {
		for !strings.HasPrefix(w, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// Terminal mode switching uses the standard `stty` tool, so no extra Go
// dependency is needed. The restore step is kept in a package variable so a
// Ctrl-C handler can also run it: os.Exit skips deferred calls, and a
// terminal left without echo is very confusing.
var (
	ttyMu      sync.Mutex
	ttyRestore func()
)

// enterRawMode turns off line buffering and echo. It fails if stdin isn't a
// terminal. Call the returned function to restore the terminal.
func enterRawMode() (func(), error) {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return nil, errors.New("stdin is not a terminal")
	}
	stty := func(args ...string) ([]byte, error) {
		cmd := exec.Command("stty", args...)
		cmd.Stdin = os.Stdin
		return cmd.Output()
	}

	saved, err := stty("-g") // current settings, in a form stty can reload
	if err != nil {
		return nil, fmt.Errorf("stty: %w", err)
	}
	state := strings.TrimSpace(string(saved))
	if _, err := stty("-icanon", "-echo", "min", "1"); err != nil {
		return nil, fmt.Errorf("stty: %w", err)
	}

	ttyMu.Lock()
	ttyRestore = func() { stty(state) }
	ttyMu.Unlock()
	return restoreTerminal, nil
}

// restoreTerminal undoes enterRawMode. It is safe to call more than once, and
// does nothing if the terminal isn't in raw mode.
func restoreTerminal() {
	ttyMu.Lock()
	defer ttyMu.Unlock()
	if ttyRestore != nil {
		ttyRestore()
		ttyRestore = nil
	}
}
