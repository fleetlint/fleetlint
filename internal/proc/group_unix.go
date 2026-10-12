//go:build unix

package proc

import (
	"os/exec"
	"syscall"
)

// ownGroup starts the command in a process group of its own and kills the
// group on cancel.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
