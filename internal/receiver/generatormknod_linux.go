package receiver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/gokrazy/rsync"
	"golang.org/x/sys/unix"
)

func (rt *Transfer) createDevice(f *File, st fs.FileInfo) error {
	base := filepath.Base(f.Name)
	parentDir, err := rt.DestRoot.OpenFile(filepath.Dir(f.Name), 0, 0)
	if err != nil {
		return fmt.Errorf("Open(parent(%s)): %v", f.Name, err)
	}
	defer parentDir.Close()
	perm := fs.FileMode(f.Mode) & os.ModePerm
	mode := f.Mode & rsync.S_IFMT
	switch mode {
	case rsync.S_IFCHR:
		if st != nil && st.Mode().Type()&os.ModeCharDevice != 0 {
			return nil // file of correct type exists
		}
		return unix.Mknodat(int(parentDir.Fd()), base, uint32(perm)|syscall.S_IFCHR, int(f.Rdev))

	case rsync.S_IFBLK:
		if st != nil && (st.Mode().Type()&os.ModeDevice != 0 ||
			st.Mode().Type()&os.ModeCharDevice != 0) {
			return nil // file of correct type exists
		}

		return unix.Mknodat(int(parentDir.Fd()), base, uint32(perm)|syscall.S_IFBLK, int(f.Rdev))

	case rsync.S_IFSOCK:
		if st != nil && st.Mode().Type()&os.ModeSocket != 0 {
			return nil // file of correct type exists
		}

		fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
		if err != nil {
			return err
		}

		// The parent dir is safely resolved through *os.Root,
		// so we skip path resolution by constructing a path
		// from a known-safe prefix (/proc/self/fd/<parent-dir-fd>)
		// and a basename (not a path!).
		local := filepath.Join("/proc/self/fd", strconv.Itoa(int(parentDir.Fd())), base)
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

		return unix.Mkfifoat(int(parentDir.Fd()), base, uint32(perm))
	}
	return nil
}
