//go:build unix

package clientlaunchcalls

import (
	"os"
	"os/exec"
	"syscall"
)

func SetupProcAttr(cmd *exec.Cmd) (*os.File, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdin = devnull
		cmd.Stdout = devnull
		cmd.Stderr = devnull
		return devnull, nil
	}
	return nil, err
}
