//go:build windows

package receiver

import "os"

func symlink(oldname, newname string) error {
	// TODO: use os.Root.Remove
	if err := os.Remove(newname); err != nil && !os.IsNotExist(err) {
		return err
	}
	// TODO(go1.25): use os.Root.Symlink
	return os.Symlink(oldname, newname)
}
