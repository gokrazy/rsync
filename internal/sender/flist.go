package sender

import (
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/rsyncchecksum"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

type file struct {
	root    *os.Root
	path    string
	Wpath   string
	regular bool

	// fields below are used by the receiver (TODO: unify)
	Name       string
	Length     int64
	ModTime    time.Time
	Mode       int32
	Uid        int32
	Gid        int32
	LinkTarget string
	Rdev       int32
}

type fileList struct {
	TotalSize int64
	Files     []file
	Roots     []*os.Root
}

// A fileList must not be used after calling Close().
func (fl *fileList) Close() {
	for _, root := range fl.Roots {
		root.Close()
	}
	fl.Roots = nil
}

// rsync/rsync.h defines chunkSize as 32 * 1024, but increasing it to 256K
// increases throughput with “tridge” rsync as client by 50 Mbit/s.
const chunkSize = 256 * 1024

var (
	lookupOnce      sync.Once
	lookupGroupOnce sync.Once
)

func getRootStrip(requested, localDir string) (string, string) {
	root := filepath.Clean(localDir + "/" + requested)

	strip := filepath.Dir(filepath.Clean(root)) + "/"
	if strings.HasSuffix(requested, "/") {
		strip = filepath.Clean(root) + "/"
	}
	return root, strip
}

// rsync/flist.c:send_file_list
func (st *Transfer) SendFileList(localDir string, opts *rsyncopts.Options, paths []string, excl *filterRuleList) (*fileList, error) {
	var fileList fileList
	fec := &rsyncwire.Buffer{}

	uidMap := make(map[int32]string)
	gidMap := make(map[int32]string)

	// TODO: flush in between to keep the pipes filled when traversal takes long

	// TODO: handle info == nil case (permission denied?): should set an i/o
	// error flag, but traversal should continue

	if opts.Verbose() { // TODO: DebugGTE(FLIST, 1)
		st.Logger.Printf("sendFileList()")
	}
	ioErrors := int32(0)

	ioError := func(err error) {
		if os.IsNotExist(err) {
			st.Logger.Printf("file vanished: %v", err)
		} else {
			st.Logger.Printf("lstat: %v", err)
		}
		ioErrors = 1
	}

	// TODO: handle |root| referring to an individual file, symlink or special (skip)
	for _, requested := range paths {
		if opts.Verbose() { // TODO: DebugGTE(FLIST, 1)
			st.Logger.Printf("  path %q (local dir %q)", requested, localDir)
		}
		// st.Logger.Printf("getRootStrip(requested=%q, localDir=%q", requested, localDir)
		rootPath, strip := getRootStrip(requested, localDir)
		// st.Logger.Printf("root=%q, strip=%q", root, strip)
		prefix := strings.TrimPrefix(rootPath, filepath.Clean(strip))
		if prefix != "" {
			prefix = strings.TrimPrefix(prefix, "/")
			prefix += "/"
		}
		if opts.Verbose() { // TODO: DebugGTE(FLIST, 1)
			st.Logger.Printf("  filepath.Walk(%q), strip=%q", rootPath, strip)
			st.Logger.Printf("  prefix=%q", prefix)
		}

		root, err := os.OpenRoot(rootPath)
		if err != nil {
			// set the I/O error flag, but keep going
			ioError(err)
			continue
		}
		fileList.Roots = append(fileList.Roots, root)
		err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
			st.Logger.Printf("filepath.WalkFn(path=%s)", path)
			var info fs.FileInfo
			if err == nil {
				info, err = d.Info()
			}
			if err != nil {
				// set the I/O error flag, but keep walking
				ioError(err)
				return nil
			}

			// Only ever transmit long names, like openrsync
			flags := byte(rsync.XMIT_LONG_NAME)

			name := prefix + path
			st.Logger.Printf("Trim(path=%q, %q) = %q", path, strip, name)
			if path == "." {
				name = prefix
				if prefix == "" {
					name = "."
				}
				flags |= rsync.XMIT_TOP_DIR
			}
			// st.logger.Printf("flags for %q: %v", name, flags)

			if excl.matches(name) {
				return filepath.SkipDir
			}

			fileList.Files = append(fileList.Files, file{
				root:    root,
				path:    path,
				regular: info.Mode().IsRegular(),
				Wpath:   name,
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

			fileList.TotalSize += size

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

			if opts.PreserveUid() {
				uid, ok := uidFromFileInfo(info)
				if ok {
					if _, ok := uidMap[uid]; !ok && uid != 0 {
						u, err := user.LookupId(strconv.Itoa(int(uid)))
						if err != nil {
							lookupOnce.Do(func() {
								st.Logger.Printf("lookup(%d) = %v", uid, err)
							})
						} else {
							uidMap[uid] = u.Username
						}
					}
				}
				// 8.   if -o, the user id (integer)
				fec.WriteInt32(uid)
			}

			if opts.PreserveGid() {
				gid, ok := gidFromFileInfo(info)
				if ok {
					if _, ok := gidMap[gid]; !ok && gid != 0 {
						g, err := user.LookupGroupId(strconv.Itoa(int(gid)))
						if err != nil {
							lookupGroupOnce.Do(func() {
								st.Logger.Printf("lookupgroup(%d) = %v", gid, err)
							})
						} else {
							gidMap[gid] = g.Name
						}
					}
				}
				// 9.   if -g, the group id (integer)
				fec.WriteInt32(gid)
			}

			if (opts.PreserveDevices() && isDev) ||
				(opts.PreserveSpecials() && isSpecial) {
				// 10.  if a special file and -D, the device “rdev” type (integer)
				rdev, _ := rdevFromFileInfo(info)
				fec.WriteInt32(rdev)
			}

			if opts.PreserveLinks() && info.Mode().Type()&os.ModeSymlink != 0 {
				// 11.  if a symbolic link and -l, the link target's length (integer)
				// 12.  if a symbolic link and -l, the link target (byte array)

				// TODO(go1.25): use fl.root.Readlink(fl.path)
				target, err := os.Readlink(filepath.Join(rootPath, path))
				if err != nil {
					return err // TODO
				}
				fec.WriteInt32(int32(len(target)))
				fec.WriteString(target)
			}

			if opts.AlwaysChecksum() {
				var emptyChecksum [rsyncchecksum.Size]byte
				checksum := emptyChecksum[:]
				if info.Mode().IsRegular() {
					// TODO: send md4 checksum of this file
					checksum, err = rsyncchecksum.FileChecksum(root, path)
					if err != nil {
						return err
					}
				} else {
					// send empty md4 checksum
				}
				fec.WriteString(string(checksum))
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

	fec.WriteInt32(ioErrors)

	if err := st.Conn.WriteString(fec.String()); err != nil {
		return nil, err
	}

	return &fileList, nil
}
