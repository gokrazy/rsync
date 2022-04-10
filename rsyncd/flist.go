package rsyncd

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

var (
	lookupOnce      sync.Once
	lookupGroupOnce sync.Once
)

// rsync/flist.c:send_file_list
func (st *sendTransfer) sendFileList(mod Module, opts *Opts, paths []string) (*fileList, error) {
	var fileList fileList
	fec := &rsyncwire.Buffer{}

	uidMap := make(map[int32]string)
	gidMap := make(map[int32]string)

	// TODO: flush in between to keep the pipes filled when traversal takes long

	// TODO: handle info == nil case (permission denied?): should set an i/o
	// error flag, but traversal should continue

	st.logger.Printf("sendFileList(module=%q)", mod.Name)
	// TODO: handle |root| referring to an individual file, symlink or special (skip)
	for _, requested := range paths {
		modRoot := mod.Path
		st.logger.Printf("  path %q (module root %q)", requested, modRoot)
		root := strings.TrimPrefix(requested, mod.Name+"/")
		root = filepath.Clean(mod.Path + "/" + root)
		// st.logger.Printf("  filepath.Walk(%q)", root)
		strip := filepath.Dir(filepath.Clean(root)) + "/"
		if strings.HasSuffix(requested, "/") {
			strip = filepath.Clean(root) + "/"
		}
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			// st.logger.Printf("filepath.WalkFn(path=%s)", path)
			if err != nil {
				return err
			}

			// Only ever transmit long names, like openrsync
			flags := byte(rsync.XMIT_LONG_NAME)

			name := strings.TrimPrefix(path, strip)
			// st.logger.Printf("Trim(path=%q, %q) = %q", path, strip, name)
			if name == root {
				name = "."
				flags |= rsync.XMIT_TOP_DIR
			}
			// st.logger.Printf("flags for %q: %v", name, flags)

			fileList.files = append(fileList.files, file{
				path:    path,
				regular: info.Mode().IsRegular(),
				wpath:   name,
			})

			// 1.   status byte (integer)
			fec.WriteByte(flags)

			// 2.   inherited filename length (optional, byte)
			// 3.   filename length (integer or byte)
			fec.WriteInt32(int32(len(name)))

			// 4.   file (byte array)
			fec.WriteString(name)

			// 5.   file length (long)
			size := info.Size()
			if info.Mode().IsDir() {
				// tmpfs returns non-4K sizes for directories. Override with
				// 4096 to make the tests succeed regardless of the /tmp file
				// system type.
				size = 4096
			}
			fec.WriteInt64(size)

			fileList.totalSize += size

			// 6.   file modification time (optional, integer)
			// TODO: this will overflow in 2038! :(
			fec.WriteInt32(int32(info.ModTime().Unix()))

			// 7.   file mode (optional, mode_t, integer)
			mode := int32(info.Mode() & os.ModePerm)
			isDev := false
			isSpecial := false
			if info.Mode().IsDir() {
				mode |= rsync.S_IFDIR
			} else if info.Mode().IsRegular() {
				mode |= rsync.S_IFREG
			} else if info.Mode().Type()&os.ModeSymlink != 0 {
				mode |= rsync.S_IFLNK
				// TODO: skip symlink if PreserveSymlinks is not set
			}

			if info.Mode().Type()&os.ModeCharDevice != 0 {
				mode |= rsync.S_IFCHR
				isDev = true
			} else if info.Mode().Type()&os.ModeDevice != 0 {
				mode |= rsync.S_IFBLK
				isDev = true
			}

			if info.Mode().Type()&os.ModeNamedPipe != 0 {
				mode |= rsync.S_IFIFO
				isSpecial = true
			}

			if info.Mode().Type()&os.ModeSocket != 0 {
				mode |= rsync.S_IFSOCK
				isSpecial = true
			}

			fec.WriteInt32(mode)

			if opts.PreserveUid {
				uid, ok := uidFromFileInfo(info)
				if ok {
					if _, ok := uidMap[uid]; !ok && uid != 0 {
						u, err := user.LookupId(strconv.Itoa(int(uid)))
						if err != nil {
							lookupOnce.Do(func() {
								st.logger.Printf("lookup(%d) = %v", uid, err)
							})
						} else {
							uidMap[uid] = u.Username
						}
					}
				}
				// 8.   if -o, the user id (integer)
				fec.WriteInt32(uid)
			}

			if opts.PreserveGid {
				gid, ok := gidFromFileInfo(info)
				if ok {
					if _, ok := gidMap[gid]; !ok && gid != 0 {
						g, err := user.LookupGroupId(strconv.Itoa(int(gid)))
						if err != nil {
							lookupGroupOnce.Do(func() {
								st.logger.Printf("lookupgroup(%d) = %v", gid, err)
							})
						} else {
							gidMap[gid] = g.Name
						}
					}
				}
				// 9.   if -g, the group id (integer)
				fec.WriteInt32(gid)
			}

			if (opts.PreserveDevices && isDev) ||
				(opts.PreserveSpecials && isSpecial) {
				// 10.  if a special file and -D, the device “rdev” type (integer)
				rdev, _ := rdevFromFileInfo(info)
				fec.WriteInt32(rdev)
			}

			if opts.PreserveLinks && info.Mode().Type()&os.ModeSymlink != 0 {
				// 11.  if a symbolic link and -l, the link target's length (integer)
				// 12.  if a symbolic link and -l, the link target (byte array)
				target, err := os.Readlink(path)
				if err != nil {
					return err // TODO
				}
				fec.WriteInt32(int32(len(target)))
				fec.WriteString(target)
			}

			// The status byte may consist of the following bits and determines which of the optional fields are transmitted.

			// 0x01    A top-level directory.  (Only applies to directory files.)  If specified, the matching local directory is for deletions.
			// 0x02    Do not send the file mode: it is a repeat of the last file's mode.
			// 0x08    Like 0x02, but for the user id.
			// 0x10    Like 0x02, but for the group id.
			// 0x20    Inherit some of the prior file name.  Enables the inherited filename length transmission.
			// 0x40    Use full integer length for file name.  Otherwise, use only the byte length.
			// 0x80    Do not send the file modification time: it is a repeat of the last file's.

			// If the status byte is zero, the file-list has terminated.

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	const endOfFileList = 0
	fec.WriteByte(endOfFileList)

	const endOfSet = 0
	for uid, name := range uidMap {
		fec.WriteInt32(uid)
		fec.WriteByte(byte(len(name)))
		fec.WriteString(name)
	}
	fec.WriteInt32(endOfSet)
	for gid, name := range gidMap {
		fec.WriteInt32(gid)
		fec.WriteByte(byte(len(name)))
		fec.WriteString(name)
	}
	fec.WriteInt32(endOfSet)

	const ioErrors = 0
	fec.WriteInt32(ioErrors)

	if err := st.conn.WriteString(fec.String()); err != nil {
		return nil, err
	}

	return &fileList, nil
}
