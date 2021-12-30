//go:build linux || darwin

package receivermaincmd

import "github.com/google/renameio/v2"

func symlink(oldname, newname string) error {
	return renameio.Symlink(oldname, newname)
}
