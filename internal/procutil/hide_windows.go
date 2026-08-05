//go:build windows

package procutil

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW prevents console flashes when spawning adb/taskkill etc.
const createNoWindow = 0x08000000

// HideConsole configures cmd so no console window appears on Windows.
func HideConsole(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
