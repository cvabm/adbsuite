//go:build !windows

package procutil

import "os/exec"

// HideConsole is a no-op outside Windows.
func HideConsole(cmd *exec.Cmd) {}
