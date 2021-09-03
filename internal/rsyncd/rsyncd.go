package rsyncd

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/DavidGamba/go-getoptions"
	"github.com/stapelberg/rsync-os/rsync"
	"golang.org/x/crypto/md4"
)

type Module struct {
	Path string
}

type Server struct {
	Modules map[string]Module
}

func (s *Server) getModule(requestedModule string) (Module, error) {
	m, ok := s.Modules[requestedModule]
	if !ok {
		return Module{}, fmt.Errorf("no such module")
	}
	return m, nil
}

func (s *Server) formatModuleList() string {
	if len(s.Modules) == 0 {
		return ""
	}
	var list strings.Builder
	for name := range s.Modules {
		comment := name // for now
		fmt.Fprintf(&list, "%s\t%s\n",
			name,
			comment)
	}
	return list.String()
}

type file struct {
	// TODO: store relative to the root to conserve RAM
	path    string
	regular bool
}

func (s *Server) sendFileList(c *rsyncConn, root string, opts rsyncOpts) ([]file, error) {
	var fileList []file
	fec := &rsyncBuffer{}

	uidMap := make(map[int32]string)
	gidMap := make(map[int32]string)

	// TODO: flush in between to keep the pipes filled when traversal takes long

	// TODO: handle info == nil case (permission denied?): should set an i/o
	// error flag, but traversal should continue

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// log.Printf("filepath.WalkFn(path=%s)", path)
		if err != nil {
			return err
		}

		fileList = append(fileList, file{
			path:    path,
			regular: info.Mode().IsRegular(),
		})

		// Only ever transmit long names, like openrsync
		flags := byte(rsync.FLIST_NAME_LONG)

		name := strings.TrimPrefix(path, root+"/")
		if path == root {
			name = "."
			flags |= rsync.FLIST_TOP_LEVEL
		}
		// log.Printf("flags for %s: %v", name, flags)

		// 1.   status byte (integer)
		fec.writeByte(flags)

		// 2.   inherited filename length (optional, byte)
		// 3.   filename length (integer or byte)
		fec.writeInt32(int32(len(name)))

		// 4.   file (byte array)
		fec.writeString(name)

		// 5.   file length (long)
		fec.writeInt64(info.Size())

		// 6.   file modification time (optional, integer)
		// TODO: this will overflow in 2038! :(
		fec.writeInt32(int32(info.ModTime().Unix()))

		// 7.   file mode (optional, mode_t, integer)
		mode := int32(info.Mode() & os.ModePerm)
		isDev := false
		isSpecial := false
		// as per /usr/include/bits/stat.h:
		const (
			S_IFDIR  = 0o0040000 // Directory
			S_IFCHR  = 0o0020000 // Character device
			S_IFBLK  = 0o0060000 // Block device
			S_IFREG  = 0o0100000 // Regular file
			S_IFIFO  = 0o0010000 // FIFO
			S_IFLNK  = 0o0120000 // Symbolic link
			S_IFSOCK = 0o0140000 // Socket
		)
		if info.Mode().IsDir() {
			mode |= S_IFDIR
		} else if info.Mode().IsRegular() {
			mode |= S_IFREG
		} else if info.Mode().Type()&os.ModeSymlink != 0 {
			mode |= S_IFLNK
		}

		if info.Mode().Type()&os.ModeCharDevice != 0 {
			mode |= S_IFCHR
			isDev = true
		} else if info.Mode().Type()&os.ModeDevice != 0 {
			mode |= S_IFBLK
			isDev = true
		}

		if info.Mode().Type()&os.ModeNamedPipe != 0 {
			mode |= S_IFIFO
			isSpecial = true
		}

		if info.Mode().Type()&os.ModeSocket != 0 {
			mode |= S_IFSOCK
			isSpecial = true
		}

		fec.writeInt32(mode)

		if opts.PreserveUid {
			var uid int32
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				uid = int32(st.Uid)
				if _, ok := uidMap[uid]; !ok && uid != 0 {
					u, err := user.LookupId(strconv.Itoa(int(uid)))
					if err != nil {
						log.Printf("lookup(%d) = %v", uid, err)
					} else {
						uidMap[uid] = u.Username
					}
				}
			}
			// 8.   if -o, the user id (integer)
			fec.writeInt32(uid)
		}

		if opts.PreserveGid {
			var gid int32
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				gid = int32(st.Gid)
				if _, ok := gidMap[gid]; !ok && gid != 0 {
					g, err := user.LookupGroupId(strconv.Itoa(int(gid)))
					if err != nil {
						log.Printf("lookupgroup(%d) = %v", gid, err)
					} else {
						gidMap[gid] = g.Name
					}
				}
			}
			// 9.   if -g, the group id (integer)
			fec.writeInt32(gid)
		}

		if (opts.PreserveDevices && isDev) ||
			(opts.PreserveSpecials && isSpecial) {
			// 10.  if a special file and -D, the device “rdev” type (integer)
			sys, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("stat does not contain rdev")
			}
			fec.writeInt32(int32(sys.Rdev))
		}

		if opts.PreserveLinks && info.Mode().Type()&os.ModeSymlink != 0 {
			// 11.  if a symbolic link and -l, the link target's length (integer)
			// 12.  if a symbolic link and -l, the link target (byte array)
			target, err := os.Readlink(path)
			if err != nil {
				return err // TODO
			}
			fec.writeInt32(int32(len(target)))
			fec.writeString(target)
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

	const endOfFileList = 0
	fec.writeByte(endOfFileList)

	const endOfSet = 0
	for uid, name := range uidMap {
		fec.writeInt32(uid)
		fec.writeByte(byte(len(name)))
		fec.writeString(name)
	}
	fec.writeInt32(endOfSet)
	for gid, name := range gidMap {
		fec.writeInt32(gid)
		fec.writeByte(byte(len(name)))
		fec.writeString(name)
	}
	fec.writeInt32(endOfSet)

	const ioErrors = 0
	fec.writeInt32(ioErrors)

	if err := c.writeString(fec.buf.String()); err != nil {
		return nil, err
	}

	return fileList, nil
}

type rsyncOpts struct {
	Server           bool
	Sender           bool
	PreserveGid      bool
	PreserveUid      bool
	PreserveLinks    bool
	PreservePerms    bool
	PreserveDevices  bool
	PreserveSpecials bool
	PreserveTimes    bool
	Recurse          bool
	IgnoreTimes      bool
}

func (c *rsyncConn) sendFile(fileIndex int32, fl file) error {
	const chunkSize = 32 * 1024 // rsync/rsync.h

	f, err := os.Open(fl.path)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	if err := c.writeInt32(fileIndex); err != nil {
		return err
	}

	sh := sumSizesSqroot(fi.Size())
	// log.Printf("sh = %+v", sh)
	if err := c.writeSumHead(sh); err != nil {
		return err
	}

	h := md4.New()
	binary.Write(h, binary.LittleEndian, c.seed)
	buf := make([]byte, chunkSize)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		chunk := buf[:n]
		// chunk size (“rawtok” variable in openrsync)
		if err := c.writeInt32(int32(len(chunk))); err != nil {
			return err
		}
		if _, err := c.wr.Write(chunk); err != nil {
			return err
		}
		h.Write(chunk)
	}
	// transfer finished:
	if err := c.writeInt32(0); err != nil {
		return err
	}

	// whole file long checksum (16 bytes)
	sum := h.Sum(nil)
	// log.Printf("sum: %x (len = %d)", sum, len(sum))
	if _, err := c.wr.Write(sum); err != nil {
		return err
	}
	return nil
}

// rsync/sender.c:send_files()
func (c *rsyncConn) sendFiles(fileList []file) error {
	for {
		// receive data about receiver’s copy of the file list contents (not
		// ordered)
		// see (*rsync.Receiver).Generator()
		fileIndex, err := c.readInt32()
		if err != nil {
			return err
		}
		if fileIndex == -1 {
			// acknowledge phase change by sending -1
			if err := c.writeInt32(-1); err != nil {
				return err
			}
			break
		}
		// log.Printf("fileIndex: %v (hex %x)", fileIndex, fileIndex)
		sumHead, err := c.readSumHead()
		if err != nil {
			return err
		}
		// log.Printf("sum head: %+v", sumHead)
		longChecksum := make([]byte, sumHead.ChecksumLength)
		for i := int32(0); i < sumHead.ChecksumCount; i++ {
			shortChecksum, err := c.readInt32()
			if err != nil {
				return err
			}
			n, err := c.rd.Read(longChecksum)
			if err != nil {
				return err
			}
			_ = shortChecksum
			_ = n
			// log.Printf("short %d, long %x", shortChecksum, longChecksum[:n])
		}

		// TODO(optimization): only send data that has changed (based on the
		// checksums received above)

		if err := c.sendFile(fileIndex, fileList[fileIndex]); err != nil {
			if _, ok := err.(*os.PathError); ok {
				// OpenFile() failed. Log the error (server side only) and
				// proceed. Only starting with protocol 30, an I/O error flag is
				// sent after the file transfer phase.
				if os.IsNotExist(err) {
					log.Printf("file has vanished: %s", fileList[fileIndex].path)
				} else {
					log.Printf("sendFiles: %v", err)
				}
				continue
			} else {
				return err
			}
		}
	}

	// phase done
	if err := c.writeInt32(-1); err != nil {
		return err
	}

	return nil
}

func (s *Server) handleConn(conn net.Conn) (err error) {
	const terminationCommand = "@RSYNCD: OK\n"
	rd := bufio.NewReader(conn)
	// send server greeting

	// protocol version 27 seems to be the safest bet for wide compatibility:
	// version 27 was introduced by rsync 2.6.0 (released 2004), and is
	// supported by openrsync and rsyn.
	const protocolVersion = "27"
	fmt.Fprintf(conn, "@RSYNCD: %s\n", protocolVersion)

	// read client greeting
	clientGreeting, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(clientGreeting, "@RSYNCD: ") {
		return fmt.Errorf("invalid client greeting: got %q", clientGreeting)
	}

	// read requested module(s), if any
	requestedModule, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	requestedModule = strings.TrimSpace(requestedModule)
	if requestedModule == "" || requestedModule == "#list" {
		log.Printf("client requested rsync module listing")
		io.WriteString(conn, s.formatModuleList())
		io.WriteString(conn, "@RSYNCD: EXIT\n")
		return nil
	}
	log.Printf("client requested rsync module %q", requestedModule)
	module, err := s.getModule(requestedModule)
	if err != nil {
		fmt.Fprintf(conn, "@ERROR: Unknown module '%s'\n", requestedModule)
		return err
	}

	io.WriteString(conn, terminationCommand)

	// read requested flags
	var flags []string
	for {
		flag, err := rd.ReadString('\n')
		if err != nil {
			return err
		}
		flag = strings.TrimSpace(flag)
		log.Printf("client sent: %q", flag)
		if flag == "" {
			break
		}
		flags = append(flags, flag)
	}

	log.Printf("flags: %+v", flags)
	var opts rsyncOpts
	// rsync itself uses /usr/include/popt.h for option parsing
	opt := getoptions.New()

	// rsync (but not openrsync) bundles short options together, i.e. it sends
	// e.g. -logDtpr
	opt.SetMode(getoptions.Bundling)

	opt.BoolVar(&opts.Server, "server", false)
	opt.BoolVar(&opts.Sender, "sender", false)
	opt.BoolVar(&opts.PreserveGid, "group", false, opt.Alias("g"))
	opt.BoolVar(&opts.PreserveUid, "owner", false, opt.Alias("o"))
	opt.BoolVar(&opts.PreserveLinks, "links", false, opt.Alias("l"))
	opt.BoolVar(&opts.PreservePerms, "perms", false, opt.Alias("p"))
	dOpt := opt.Bool("D", false)
	opt.BoolVar(&opts.Recurse, "recursive", false, opt.Alias("r"))
	opt.BoolVar(&opts.PreserveTimes, "times", false, opt.Alias("t"))
	opt.Bool("v", false)     // verbosity; ignored
	opt.Bool("debug", false) // debug; ignored
	opt.BoolVar(&opts.IgnoreTimes, "ignore-times", false, opt.Alias("I"))

	//getoptions.Debug.SetOutput(os.Stderr)
	remaining, err := opt.Parse(flags)
	if err != nil {
		// TODO: terminate connection with an error about which flag is not
		// supported
		return fmt.Errorf("opt.Parse: %v", err)
	}
	if *dOpt {
		opts.PreserveDevices = true
		opts.PreserveSpecials = true
	}
	log.Printf("remaining: %q", remaining)
	// TODO: verify --sender is set and error out otherwise

	// “SHOULD be unique to each connection” as per
	// https://github.com/JohannesBuchner/Jarsync/blob/master/jarsync/rsync.txt
	//
	// TODO: random seed
	// TODO: from which source?
	const sessionChecksumSeed = 666

	c := &rsyncConn{
		seed: sessionChecksumSeed,
		rd:   rd,
		wr:   conn,
	}

	if err := c.writeInt32(c.seed); err != nil {
		return err
	}

	// Switch to multiplexing protocol, but only for server-side transmissions.
	// Transmissions received from the client are not multiplexed.
	mpx := &multiplexWriter{underlying: c.wr}
	c.wr = mpx
	// If returning an error, send the error to the client for display, too:
	defer func() {
		if err != nil {
			mpx.WriteMsg(msgError, []byte(fmt.Sprintf("gokr-rsync [sender]: %v\n", err)))
		}
	}()

	// receive the exclusion list (openrsync’s is always empty)
	const exclusionListEnd = 0
	// got, err := c.readInt32()
	// if err != nil {
	// 	return err
	// }
	// if want := int32(exclusionListEnd); got != want {
	// 	return fmt.Errorf("protocol error: non-empty exclusion list received")
	// }

	// log.Printf("exclusion list read")

	// “Update exchange” as per
	// https://github.com/kristapsdz/openrsync/blob/master/rsync.5

	// send file list
	fileList, err := s.sendFileList(c, module.Path, opts)
	if err != nil {
		return err
	}

	log.Printf("file list sent")

	// TODO: read exclusion list (always zero)
	got, err := c.readInt32()
	if err != nil {
		return err
	}
	if want := int32(exclusionListEnd); got != want {
		return fmt.Errorf("protocol error: non-empty exclusion list received")
	}

	log.Printf("exclusion list read")

	if err := c.sendFiles(fileList); err != nil {
		return err
	}

	// TODO: make this conditional
	// send statistics
	if err := c.writeInt64(1234); err != nil {
		return err
	}
	if err := c.writeInt64(5678); err != nil {
		return err
	}
	if err := c.writeInt64(666); err != nil {
		return err
	}

	finish, err := c.readInt32()
	if err != nil {
		return err
	}
	if finish != -1 {
		return fmt.Errorf("protocol error: expected final -1, got %d", finish)
	}
	return nil
}

func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			if err := s.handleConn(conn); err != nil {
				log.Printf("[%s] handle: %v", conn.RemoteAddr(), err)
			}
		}()
	}
}

const blockSize = 700 // rsync/rsync.h

// Corresponds to rsync/generator.c:sum_sizes_sqroot
func sumSizesSqroot(len int64) sumHead {
	// * The block size is a rounded square root of file length.

	// 	The block size algorithm plays a crucial role in the protocol efficiency. In general, the block size is the rounded square root of the total file size. The minimum block size, however, is 700 B. Otherwise, the square root computation is simply sqrt(3) followed by ceil(3)

	// For reasons unknown, the square root result is rounded up to the nearest multiple of eight.

	// TODO: round this
	blockLength := int32(math.Sqrt(float64(len)))
	if blockLength < blockSize {
		blockLength = blockSize
	}

	// * The checksum size is determined according to:
	// *     blocksum_bits = BLOCKSUM_EXP + 2*log2(file_len) - log2(block_len)
	// * provided by Donovan Baarda which gives a probability of rsync
	// * algorithm corrupting data and falling back using the whole md4
	// * checksums.
	const checksumLength = 16 // TODO?

	return sumHead{
		ChecksumCount:   int32((len + (int64(blockLength) - 1)) / int64(blockLength)),
		RemainderLength: int32(len % int64(blockLength)),
		BlockLength:     blockLength,
		ChecksumLength:  checksumLength,
	}
}
