//go:build !windows

package modelsupervisor

import "os/exec"

func withoutConsole(cmd *exec.Cmd) *exec.Cmd { return cmd }
