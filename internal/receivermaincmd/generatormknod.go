//go:build linux || darwin

package receivermaincmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/gokrazy/rsync"
	"golang.org/x/sys/unix"
)

func (rt *recvTransfer) createDevice(f *file, st fs.FileInfo) error {
	local := filepath.Join(rt.dest, f.Name)
	perm := fs.FileMode(f.Mode) & os.ModePerm
	mode := f.Mode & rsync.S_IFMT
	switch mode {
	case rsync.S_IFCHR:
		if st != nil && st.Mode().Type()&os.ModeCharDevice != 0 {
			return nil // file of correct type exists
		}
		return unix.Mknod(local, uint32(perm)|syscall.S_IFCHR, int(f.Rdev))

	case rsync.S_IFBLK:
		if st != nil && (st.Mode().Type()&os.ModeDevice != 0 ||
			st.Mode().Type()&os.ModeCharDevice != 0) {
			return nil // file of correct type exists
		}

		return unix.Mknod(local, uint32(perm)|syscall.S_IFBLK, int(f.Rdev))

	case rsync.S_IFSOCK:
		if st != nil && st.Mode().Type()&os.ModeSocket != 0 {
			return nil // file of correct type exists
		}

		fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
		if err != nil {
			return err
		}

		if err := unix.Bind(fd, &unix.SockaddrUnix{Name: local}); err != nil {
			return err
		}

		if err := unix.Close(fd); err != nil {
			return err
		}
		return nil

	case rsync.S_IFIFO:
		if st != nil && st.Mode().Type()&os.ModeNamedPipe != 0 {
			return nil // file of correct type exists
		}

		return unix.Mkfifo(local, uint32(perm))
	}
	return nil
}
