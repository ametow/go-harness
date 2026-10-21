//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detachFromTerminal puts the server in its own process group. Otherwise a
// Ctrl-C in the terminal is delivered to the server directly, as well as to
// us, and it dies with an ugly stack trace before we can shut it down politely
// by closing its stdin.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
