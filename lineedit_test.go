package main

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
)

// typeKeys feeds keystrokes to the editor and returns the line plus everything drawn.
func typeKeys(keys string) (line string, drawn string, err error) {
	var out bytes.Buffer
	line, err = editLine(bufio.NewReader(strings.NewReader(keys)), &out, "> ", matchCommands, describeCommand)
	return line, out.String(), err
}

func TestEditLineEditing(t *testing.T) {
	tests := []struct {
		name string
		keys string
		want string
	}{
		{"plain text", "hello\r", "hello"},
		{"newline also ends the line", "hello\n", "hello"},
		{"tab completes a unique command", "/no\t\r", "/notes"},
		{"tab leaves the text alone when several commands match", "/\t\r", "/"},
		{"tab does nothing on ordinary text", "he\tllo\r", "hello"},
		{"tab does nothing on a path", "/etc/h\t\r", "/etc/h"},
		{"backspace", "/nx\x7f\x7fe\t\r", "/exit"}, // "/nx" -> "/n" -> "/" -> "/e" -> "/exit"
		{"backspace on empty line is harmless", "\x7f\x7fhi\r", "hi"},
		{"Ctrl-U clears the line", "abc\x15def\r", "def"},
		{"arrow keys are ignored", "hi\x1b[D\x1b[Aok\r", "hiok"},
		{"unicode", "héllo\x7f\r", "héll"},
		{"other control keys are ignored", "a\x01\x02b\r", "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := typeKeys(tt.keys)
			if err != nil || got != tt.want {
				t.Fatalf("got %q (err %v), want %q", got, err, tt.want)
			}
		})
	}
}

func TestEditLineEOF(t *testing.T) {
	if _, _, err := typeKeys("\x04"); err != io.EOF { // Ctrl-D on an empty line
		t.Fatalf("err = %v, want EOF", err)
	}
	line, _, err := typeKeys("ab\x04c\r") // Ctrl-D mid-line is ignored
	if err != nil || line != "abc" {
		t.Fatalf("got %q, %v", line, err)
	}
	if line, _, err := typeKeys("partial"); err != io.EOF || line != "partial" {
		t.Fatalf("input ending without Enter: got %q, %v", line, err)
	}
}

func TestEditLineHints(t *testing.T) {
	tests := []struct {
		keys   string
		want   []string // all must appear in the drawn output
		absent []string
	}{
		{"/", []string{"/help", "/clean", "/exit"}, nil},                       // several matches: just the names
		{"/no", []string{"Show the saved long-term notes"}, []string{"/help"}}, // one match: with its description
		{"/zz", []string{"no such command"}, nil},
		{"hello", nil, []string{dim}},      // ordinary text: never a hint
		{"/etc/hosts", nil, []string{dim}}, // a path is not a command
	}
	for _, tt := range tests {
		t.Run(tt.keys, func(t *testing.T) {
			_, all, _ := typeKeys(tt.keys + "\r")
			// Every keystroke redraws the line; only the last frame is what
			// the user is left looking at ("/etc" had a hint, "/etc/" does not).
			frames := strings.Split(all, clearLine)
			drawn := frames[len(frames)-1]
			for _, w := range tt.want {
				if !strings.Contains(drawn, w) {
					t.Errorf("output missing %q", w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(drawn, a) {
					t.Errorf("output should not contain %q", a)
				}
			}
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"/new,/notes":  "/n",
		"/new":         "/new",
		"/help,/hello": "/hel",
		"/a,/b":        "/",
	} {
		if got := commonPrefix(strings.Split(in, ",")); got != want {
			t.Errorf("commonPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
