//go:build unix

package app

import (
	"os/exec"
	"syscall"
)

func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		// Send SIGTERM to process group
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		// Give a brief moment or escalate if needed
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
