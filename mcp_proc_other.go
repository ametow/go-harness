//go:build !unix

package main

import "os/exec"

// killGroupOnCancel is a no-op where process groups aren't available: the
// default is to kill only the top process.
func killGroupOnCancel(cmd *exec.Cmd) {}

// detachFromTerminal is a no-op where process groups aren't available.
func detachFromTerminal(cmd *exec.Cmd) {}
