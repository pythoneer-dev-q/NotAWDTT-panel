//go:build windows

package panel

import "os/exec"

func setXraySysProcAttr(cmd *exec.Cmd) {
	// no-op on Windows: Setsid is not supported
}