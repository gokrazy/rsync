//go:build linux

package maincmd

import (
	"os/exec"
	"syscall"
)

func runAsUnprivilegedUser(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: 65534,
			Gid: 65534,
		},
	}
}
