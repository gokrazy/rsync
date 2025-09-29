package receiver

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/rsyncchecksum"
	"github.com/gokrazy/rsync/internal/rsynccommon"
)

// rsync/generator.c:generate_files()
func (rt *Transfer) GenerateFiles(fileList []*File) error {
	phase := 0
	for idx, f := range fileList {
		// TODO: use a copy of f with .Mode |= S_IWUSR for directories, so
		// that we can create files within all directories.

		if err := rt.recvGenerator(idx, f); err != nil {
			return err
		}
	}
	phase++
	if rt.Opts.Verbose { // TODO: DebugGTE(genr, 1)
		rt.Logger.Printf("generateFiles phase=%d", phase)
	}
	if err := rt.Conn.WriteInt32(-1); err != nil {
		return err
	}

	// TODO: re-do any files that failed
	phase++
	if rt.Opts.Verbose { // TODO: DebugGTE(genr, 1)
		rt.Logger.Printf("generateFiles phase=%d", phase)
	}
	if err := rt.Conn.WriteInt32(-1); err != nil {
		return err
	}

	if rt.Opts.Verbose { // TODO: DebugGTE(genr, 1)
		rt.Logger.Printf("generateFiles finished")
	}
	return nil
}

// rsync/generator.c:skip_file
func (rt *Transfer) skipFile(f *File, st os.FileInfo) (bool, error) {
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
	return a.Equal(b)
}

// rsync/rsync.c:set_perms
func (rt *Transfer) setPerms(f *File) error {
	if rt.Opts.DryRun {
		return nil
	}

	st, err := rt.DestRoot.Lstat(f.Name)
	if err != nil {
		return err
	}

	perm := fs.FileMode(f.Mode) & os.ModePerm
	mode := f.Mode & rsync.S_IFMT
	if rt.Opts.PreserveTimes &&
		mode != rsync.S_IFLNK &&
		!modTimeEqual(st.ModTime(), f.ModTime) {
		if err := rt.DestRoot.Chtimes(f.Name, f.ModTime, f.ModTime); err != nil {
			return err
		}
	}

	_, err = rt.setUid(f, st)
	if err != nil {
		return err
	}

	if mode != rsync.S_IFLNK {
		if st.Mode().Perm() != perm { // only call Chmod if the permissions actually differ
			if err := rt.DestRoot.Chmod(f.Name, perm); err != nil {
				return err
			}
		}
	}

	return nil
}

// rsync/generator.c:recv_generator
func (rt *Transfer) recvGenerator(idx int, f *File) error {
	if rt.listOnly() {
		fmt.Fprintf(rt.Env.Stdout, "%s %11.0f %s %s\n",
			f.FileMode().String(),
			float64(f.Length), // TODO: rsync prints decimal separators
			f.ModTime.Format("2006/01/02 15:04:05"),
			f.Name)
		return nil
	}
	if rt.Opts.Verbose { // TODO: DebugGTE(genr, 1)
		rt.Logger.Printf("recv_generator(f=%+v)", f)
	}

	local := filepath.Join(rt.Dest, f.Name)
	st, err := rt.DestRoot.Lstat(f.Name)

	mode := f.Mode & rsync.S_IFMT
	if mode == rsync.S_IFDIR {
		if rt.Opts.DryRun {
			return nil
		}
		if err == nil && !st.IsDir() {
			// A file (not a directory) with this name exists. Delete it so that
			// we can create a directory instead.
			if err := rt.DestRoot.Remove(f.Name); err != nil {
				return fmt.Errorf("unlinking to make room for directory: %v", err)
			}
			err = fmt.Errorf("file removed")
		}
		if err != nil {
			perm := fs.FileMode(f.Mode) & os.ModePerm
			rt.Logger.Printf("MkdirAll(%s, %v)", f.Name, perm)
			if err := rt.DestRoot.MkdirAll(f.Name, perm); err != nil {
				// TODO: EEXIST is okay
				return err
			}
			// fallthrough to setPerms and return nil
		}
		if err := rt.setPerms(f); err != nil {
			return err
		}
		return nil
	}

	if rt.Opts.PreserveLinks && mode == rsync.S_IFLNK {
		// TODO: safe_symlinks option
		if err == nil {
			// local file exists, verify target matches
			if target, err := rt.DestRoot.Readlink(local); err == nil {
				rt.Logger.Printf("existing target: %q", target)
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
		rt.Logger.Printf("symlink %s -> %s", local, f.LinkTarget)
		if err := symlink(rt.DestRoot, f.LinkTarget, local); err != nil {
			return err
		}
		if err := rt.setPerms(f); err != nil {
			return err
		}
		return nil
	}

	if rt.Opts.PreserveDevices && (mode == rsync.S_IFCHR ||
		mode == rsync.S_IFBLK ||
		mode == rsync.S_IFSOCK ||
		mode == rsync.S_IFIFO) {
		if err := rt.createDevice(f, st); err != nil {
			return err
		}
		return nil
	}

	if rt.Opts.PreserveHardlinks {
		// TODO: hard link check
	}

	if !f.FileMode().IsRegular() {
		// None of the Preserve* options is enabled, so just skip over
		// non-regular files.
		return nil
	}

	requestFullFile := func() error {
		rt.Logger.Printf("requesting: %s", f.Name)
		if err := rt.Conn.WriteInt32(int32(idx)); err != nil {
			return err
		}
		if rt.Opts.DryRun {
			return nil
		}
		var sh rsync.SumHead
		if err := sh.WriteTo(rt.Conn); err != nil {
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
		if err := rt.DestRoot.Remove(f.Name); err != nil {
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
		if rt.Opts.Verbose { // TODO: InfoGTE(skip, 1)
			rt.Logger.Printf("skipping %s", local)
		}
		if err := rt.setPerms(f); err != nil {
			return err
		}
		return nil
	}

	if rt.Opts.DryRun {
		if err := rt.Conn.WriteInt32(int32(idx)); err != nil {
			return err
		}

		return nil
	}

	// TODO: if deltas are disabled, request the file in full

	in, err := rt.DestRoot.Open(f.Name)
	if err != nil {
		rt.Logger.Printf("failed to open %s, continuing: %v", local, err)
		return requestFullFile()
	}
	defer in.Close()

	rt.Logger.Printf("sending sums for: %s", f.Name)
	if err := rt.Conn.WriteInt32(int32(idx)); err != nil {
		return err
	}

	return rt.generateAndSendSums(in, st.Size())
}

// rsync/generator.c:generate_and_send_sums
func (rt *Transfer) generateAndSendSums(in *os.File, fileLen int64) error {
	sh := rsynccommon.SumSizesSqroot(fileLen)
	if err := sh.WriteTo(rt.Conn); err != nil {
		return err
	}
	buf := make([]byte, int(sh.BlockLength))
	remaining := fileLen
	for i := int32(0); i < sh.ChecksumCount; i++ {
		n1 := min(int64(sh.BlockLength), remaining)
		b := buf[:n1]
		if _, err := io.ReadFull(in, b); err != nil {
			return err
		}

		sum1 := rsyncchecksum.Checksum1(b)
		sum2 := rsyncchecksum.Checksum2(rt.Seed, b)
		if err := rt.Conn.WriteInt32(int32(sum1)); err != nil {
			return err
		}
		if _, err := rt.Conn.Writer.Write(sum2); err != nil {
			return err
		}
		remaining -= n1
	}
	return nil
}
