//go:build !unix

package main

import "os/exec"

// detachFromTerminal is a no-op where process groups aren't available.
func detachFromTerminal(cmd *exec.Cmd) {}
