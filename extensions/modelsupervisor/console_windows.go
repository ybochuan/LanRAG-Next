package modelsupervisor

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// withoutConsole retains stdout/stderr pipes while preventing a console popup.
func withoutConsole(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	return cmd
}
