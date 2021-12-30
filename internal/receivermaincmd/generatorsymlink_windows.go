//go:build windows

package receivermaincmd

import "os"

func symlink(oldname, newname string) error {
	if err := os.Remove(newname); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(oldname, newname)
}
