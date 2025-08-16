//go:build linux || darwin

package receiver

import (
	"os"

	"github.com/google/renameio/v2"
)

func symlink(_ *os.Root, oldname, newname string) error {
	return renameio.Symlink(oldname, newname)
}
