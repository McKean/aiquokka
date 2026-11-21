//go:build unix

package kiro

import (
	"os/exec"
	"syscall"
)

func killProcessTree(cmd *exec.Cmd) {
	// Setsid starts a new session (detaching /dev/tty so interactive login
	// prompts cannot hijack the terminal or wait for Enter) and makes the child
	// leader of a new process group whose pgid equals cmd.Process.Pid.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
