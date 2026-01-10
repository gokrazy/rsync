//go:build linux || darwin

package receiver

import (
	"os"

	"github.com/google/renameio/v2"
)

func symlink(root *os.Root, oldname, newname string) error {
	return renameio.SymlinkRoot(root, oldname, newname)
}
