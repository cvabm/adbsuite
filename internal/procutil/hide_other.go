//go:build !windows

package procutil

import (
	"os/exec"
	"syscall"
)

// HideConsole is a no-op outside Windows.
func HideConsole(cmd *exec.Cmd) {}

// HideConsoleInterruptible puts the child in a new process group so InterruptPID
// can signal the whole group (SIGINT) without killing the parent agent.
func HideConsoleInterruptible(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
