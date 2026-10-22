//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// killGroupOnCancel makes cancelling cmd's context kill its whole process
// group, not just the top process. Without it, `bash -c "a; b"` would leave
// `b` running after a timeout or Ctrl-C. The command gets its own group, so a
// terminal Ctrl-C reaches it only through us.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

// detachFromTerminal puts the server in its own process group. Otherwise a
// Ctrl-C in the terminal is delivered to the server directly, as well as to
// us, and it dies with an ugly stack trace before we can shut it down politely
// by closing its stdin.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
