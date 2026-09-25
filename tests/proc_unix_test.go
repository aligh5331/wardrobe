//go:build !windows

package tests

import (
	"os/exec"
	"syscall"
)

// setProcAttr puts the child in its own process group so the whole tree
// (including `go run`'s compiled server) can be signalled at once.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree kills the process group started by setProcAttr. The negative pid
// targets the group, so `go run`'s compiled child dies with it.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
