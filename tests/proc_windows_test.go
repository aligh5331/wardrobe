//go:build windows

package tests

import (
	"os/exec"
	"strconv"
)

// setProcAttr is a no-op on Windows: syscall.SysProcAttr has no Setpgid and
// the integration tests must still compile here.
func setProcAttr(cmd *exec.Cmd) {}

// killTree kills the child and its descendants. `go run` spawns the compiled
// server as a grandchild, so a plain cmd.Process.Kill would orphan it still
// holding the port; taskkill /T walks the tree.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
