//go:build windows

package procutil

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW prevents console flashes when spawning adb/taskkill etc.
const createNoWindow = 0x08000000

// CREATE_NEW_PROCESS_GROUP allows GenerateConsoleCtrlEvent(CTRL_BREAK) to
// interrupt the process (adb then forwards SIGINT to remote screenrecord).
const createNewProcessGroup = 0x00000200

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

// HideConsoleInterruptible is like HideConsole but places the process in its
// own group so InterruptPID can send CTRL_BREAK (clean stop for adb shell).
func HideConsoleInterruptible(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | createNewProcessGroup,
	}
}
