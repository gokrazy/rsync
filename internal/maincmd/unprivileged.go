//go:build !linux

package maincmd

import (
	"os/exec"
)

func runAsUnprivilegedUser(*exec.Cmd) {
}
