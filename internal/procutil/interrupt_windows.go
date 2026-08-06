//go:build windows

package procutil

import (
	"fmt"
	"syscall"
	"time"
)

const (
	ctrlCEvent     = 0
	ctrlBreakEvent = 1
)

// InterruptPID requests a graceful stop of a process started with
// HideConsoleInterruptible (CREATE_NEW_PROCESS_GROUP).
//
// Primary: CTRL_BREAK to the process group (pid == group id).
// Fallback: AttachConsole + CTRL_C (same as pressing Ctrl+C in a terminal).
// adb shell forwards that as SIGINT to remote screenrecord, which finalizes mp4.
func InterruptPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid")
	}
	k32 := syscall.NewLazyDLL("kernel32.dll")
	gen := k32.NewProc("GenerateConsoleCtrlEvent")

	// 1) CTRL_BREAK to process group created with CREATE_NEW_PROCESS_GROUP
	r1, _, err1 := gen.Call(uintptr(ctrlBreakEvent), uintptr(pid))
	if r1 != 0 {
		return nil
	}

	// 2) Attach to target console and send CTRL_C
	freeConsole := k32.NewProc("FreeConsole")
	attachConsole := k32.NewProc("AttachConsole")
	setHandler := k32.NewProc("SetConsoleCtrlHandler")

	_, _, _ = freeConsole.Call()
	r2, _, err2 := attachConsole.Call(uintptr(pid))
	if r2 == 0 {
		return fmt.Errorf("无法中断进程 %d (break: %v, attach: %v)", pid, err1, err2)
	}
	// Prevent this process from receiving the Ctrl+C we are about to generate.
	_, _, _ = setHandler.Call(0, 1)
	r3, _, err3 := gen.Call(uintptr(ctrlCEvent), 0)
	time.Sleep(150 * time.Millisecond)
	_, _, _ = setHandler.Call(0, 0)
	_, _, _ = freeConsole.Call()
	// Best-effort re-attach to parent console (ATTACH_PARENT_PROCESS = -1).
	_, _, _ = attachConsole.Call(^uintptr(0))

	if r3 == 0 {
		return fmt.Errorf("无法发送 Ctrl+C 到进程 %d: %v", pid, err3)
	}
	return nil
}
