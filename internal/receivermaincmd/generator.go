package receivermaincmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/rsyncchecksum"
	"github.com/gokrazy/rsync/internal/rsynccommon"
)

// rsync/generator.c:generate_files()
func (rt *recvTransfer) generateFiles(fileList []*file) error {
	phase := 0
	for idx, f := range fileList {
		// TODO: use a copy of f with .Mode |= S_IWUSR for directories, so
		// that we can create files within all directories.

		if err := rt.recvGenerator(idx, f); err != nil {
			return err
		}
	}
	phase++
	log.Printf("generateFiles phase=%d", phase)
	if err := rt.conn.WriteInt32(-1); err != nil {
		return err
	}

	// TODO: re-do any files that failed
	phase++
	log.Printf("generateFiles phase=%d", phase)
	if err := rt.conn.WriteInt32(-1); err != nil {
		return err
	}

	log.Printf("generateFiles finished")
	return nil
}

// rsync/generator.c:skip_file
func (rt *recvTransfer) skipFile(f *file, st os.FileInfo) (bool, error) {
	if st.Size() != f.Length {
		return false, nil
	}

	// TODO: always checksum flag

	// TODO: size only

	// TODO: ignore times

	return modTimeEqual(st.ModTime(), f.ModTime), nil
}

func modTimeEqual(a, b time.Time) bool {
	a = a.Truncate(time.Second)
	b = b.Truncate(time.Second)
	log.Printf("comparing mtime: %v vs. %v", a, b)
	return a.Equal(b)
}

// rsync/rsync.c:set_perms
func (rt *recvTransfer) setPerms(f *file) error {
	if rt.opts.DryRun {
		return nil
	}

	local := filepath.Join(rt.dest, f.Name)
	st, err := os.Lstat(local)
	if err != nil {
		return err
	}

	perm := fs.FileMode(f.Mode) & os.ModePerm
	mode := f.Mode & rsync.S_IFMT
	if rt.opts.PreserveTimes &&
		mode != rsync.S_IFLNK &&
		!modTimeEqual(st.ModTime(), f.ModTime) {
		if err := os.Chtimes(local, f.ModTime, f.ModTime); err != nil {
			return err
		}
	}

	_, err = rt.setUid(f, local, st)
	if err != nil {
		return err
	}

	if mode != rsync.S_IFLNK {
		if err := os.Chmod(local, perm); err != nil {
			return err
		}
	}

	return nil
}

// rsync/generator.c:recv_generator
func (rt *recvTransfer) recvGenerator(idx int, f *file) error {
	if rt.listOnly() {
		fmt.Fprintf(rt.env.stdout, "%s %11.0f %s %s\n",
			f.FileMode().String(),
			float64(f.Length), // TODO: rsync prints decimal separators
			f.ModTime.Format("2006/01/02 15:04:05"),
			f.Name)
		return nil
	}
	log.Printf("recv_generator(f=%+v)", f)

	local := filepath.Join(rt.dest, f.Name)
	st, err := os.Lstat(local)

	mode := f.Mode & rsync.S_IFMT
	if mode == rsync.S_IFDIR {
		if rt.opts.DryRun {
			return nil
		}
		if err == nil && !st.IsDir() {
			// A file (not a directory) with this name exists. Delete it so that
			// we can create a directory instead.
			if err := os.Remove(local); err != nil {
				return fmt.Errorf("unlinking to make room for directory: %v", err)
			}
			err = fmt.Errorf("file removed")
		}
		if err != nil {
			perm := fs.FileMode(f.Mode) & os.ModePerm
			log.Printf("MkdirAll(%s, %v)", local, perm)
			if err := os.MkdirAll(local, perm); err != nil {
				// TODO: EEXIST is okay
				return err
			}
			return nil
		}
		if err := rt.setPerms(f); err != nil {
			return err
		}
		return nil
	}

	if rt.opts.PreserveLinks && mode == rsync.S_IFLNK {
		// TODO: safe_symlinks option
		if err == nil {
			// local file exists, verify target matches
			if target, err := os.Readlink(local); err == nil {
				log.Printf("existing target: %q", target)
				if target == f.LinkTarget {
					if err := rt.setPerms(f); err != nil {
						return err
					}
					return nil // skip
				}
				// fallthrough to create or replace the symlink
			}
			// fallthrough to create or replace the symlink
		}
		log.Printf("symlink %s -> %s", local, f.LinkTarget)
		if err := symlink(f.LinkTarget, local); err != nil {
			return err
		}
		if err := rt.setPerms(f); err != nil {
			return err
		}
		return nil
	}

	if rt.opts.PreserveDevices && (mode == rsync.S_IFCHR ||
		mode == rsync.S_IFBLK ||
		mode == rsync.S_IFSOCK ||
		mode == rsync.S_IFIFO) {
		if err := rt.createDevice(f, st); err != nil {
			return err
		}
		return nil
	}

	if rt.opts.PreserveHardlinks {
		// TODO: hard link check
	}

	if !f.FileMode().IsRegular() {
		// None of the Preserve* options is enabled, so just skip over
		// non-regular files.
		return nil
	}

	requestFullFile := func() error {
		log.Printf("requesting: %s", f.Name)
		if err := rt.conn.WriteInt32(int32(idx)); err != nil {
			return err
		}
		if rt.opts.DryRun {
			return nil
		}
		var sh rsync.SumHead
		if err := sh.WriteTo(rt.conn); err != nil {
			return err
		}
		return nil
	}

	if os.IsNotExist(err) {
		return requestFullFile()
	}
	if err != nil {
		return err
	}

	if !st.Mode().IsRegular() {
		// A non-regular file with this name exists. Delete it so that we can
		// create our file instead.
		if err := os.Remove(local); err != nil {
			return fmt.Errorf("unlinking to make room for regular file: %v", err)
		}
		return requestFullFile()
	}

	// TODO: update-only check

	skip, err := rt.skipFile(f, st)
	if err != nil {
		return err
	}
	if skip {
		log.Printf("skipping %s", local)
		if err := rt.setPerms(f); err != nil {
			return err
		}
		return nil
	}

	if rt.opts.DryRun {
		if err := rt.conn.WriteInt32(int32(idx)); err != nil {
			return err
		}

		return nil
	}

	// TODO: if deltas are disabled, request the file in full

	in, err := os.Open(local)
	if err != nil {
		log.Printf("failed to open %s, continuing: %v", local, err)
		return requestFullFile()
	}
	defer in.Close()

	log.Printf("sending sums for: %s", f.Name)
	if err := rt.conn.WriteInt32(int32(idx)); err != nil {
		return err
	}

	return rt.generateAndSendSums(in, st.Size())
}

// rsync/generator.c:generate_and_send_sums
func (rt *recvTransfer) generateAndSendSums(in *os.File, fileLen int64) error {
	sh := rsynccommon.SumSizesSqroot(fileLen)
	if err := sh.WriteTo(rt.conn); err != nil {
		return err
	}
	buf := make([]byte, int(sh.BlockLength))
	remaining := fileLen
	for i := int32(0); i < sh.ChecksumCount; i++ {
		n1 := int64(sh.BlockLength)
		if n1 > remaining {
			n1 = remaining
		}
		b := buf[:n1]
		if _, err := io.ReadFull(in, b); err != nil {
			return err
		}

		sum1 := rsyncchecksum.Checksum1(b)
		sum2 := rsyncchecksum.Checksum2(rt.seed, b)
		if err := rt.conn.WriteInt32(int32(sum1)); err != nil {
			return err
		}
		if _, err := rt.conn.Writer.Write(sum2); err != nil {
			return err
		}
		remaining -= n1
	}
	return nil
}
