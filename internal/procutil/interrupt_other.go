//go:build !windows

package procutil

import (
	"fmt"
	"syscall"
)

// InterruptPID sends SIGINT to the process group (negative pid) when the child
// was started with Setpgid, matching terminal Ctrl+C behavior.
func InterruptPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid")
	}
	// Kill process group: adb + anything it spawned in the group.
	if err := syscall.Kill(-pid, syscall.SIGINT); err != nil {
		// Fallback: signal the process alone.
		return syscall.Kill(pid, syscall.SIGINT)
	}
	return nil
}
