//go:build !windows

package panel

import (
	"os/exec"
	"syscall"
)

func setXraySysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}