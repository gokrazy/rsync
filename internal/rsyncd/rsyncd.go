package rsyncd

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DavidGamba/go-getoptions"
	"github.com/gokrazy/rsync/internal/config"
	"github.com/mmcloughlin/md4"
	"github.com/stapelberg/rsync-os/rsync"
	"golang.org/x/sync/errgroup"
)

type Server struct {
	Modules map[string]config.Module
}

func (s *Server) getModule(requestedModule string) (config.Module, error) {
	m, ok := s.Modules[requestedModule]
	if !ok {
		return config.Module{}, fmt.Errorf("no such module")
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
	wpath   string
	regular bool
}

type fileList struct {
	totalSize int64
	files     []file
}

func (s *Server) sendFileList(c *rsyncConn, mod config.Module, opts *Opts, paths []string) (*fileList, error) {
	var fileList fileList
	fec := &rsyncBuffer{}

	uidMap := make(map[int32]string)
	gidMap := make(map[int32]string)

	// TODO: flush in between to keep the pipes filled when traversal takes long

	// TODO: handle info == nil case (permission denied?): should set an i/o
	// error flag, but traversal should continue

	log.Printf("sendFileList(module=%q)", mod.Name)
	// TODO: handle |root| referring to an individual file, symlink or special (skip)
	for _, requested := range paths {
		modRoot := mod.Path
		log.Printf("  path %q (module root %q)", requested, modRoot)
		root := strings.TrimPrefix(requested, mod.Name+"/")
		root = filepath.Clean(mod.Path + "/" + root)
		// log.Printf("  filepath.Walk(%q)", root)
		strip := filepath.Dir(filepath.Clean(root)) + "/"
		if strings.HasSuffix(requested, "/") {
			strip = filepath.Clean(root) + "/"
		}
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			// log.Printf("filepath.WalkFn(path=%s)", path)
			if err != nil {
				return err
			}

			// Only ever transmit long names, like openrsync
			flags := byte(rsync.FLIST_NAME_LONG)

			name := strings.TrimPrefix(path, strip)
			// log.Printf("Trim(path=%q, %q) = %q", path, strip, name)
			if name == root {
				name = "."
				flags |= rsync.FLIST_TOP_LEVEL
			}
			// log.Printf("flags for %q: %v", name, flags)

			fileList.files = append(fileList.files, file{
				path:    path,
				regular: info.Mode().IsRegular(),
				wpath:   name,
			})

			// 1.   status byte (integer)
			fec.writeByte(flags)

			// 2.   inherited filename length (optional, byte)
			// 3.   filename length (integer or byte)
			fec.writeInt32(int32(len(name)))

			// 4.   file (byte array)
			fec.writeString(name)

			// 5.   file length (long)
			fec.writeInt64(info.Size())

			fileList.totalSize += info.Size()

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
				// TODO: skip symlink if PreserveSymlinks is not set
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
				uid, ok := uidFromFileInfo(info)
				if ok {
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
				gid, ok := gidFromFileInfo(info)
				if ok {
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
				rdev, _ := rdevFromFileInfo(info)
				fec.writeInt32(rdev)
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

	return &fileList, nil
}

type Opts struct {
	Gokrazy struct {
		Listen           string
		MonitoringListen string
		AnonSSHListen    string
		ModuleMap        string
	}

	Daemon           bool
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
	DryRun           bool
	D                bool
}

func NewGetOpt() (*Opts, *getoptions.GetOpt) {
	var opts Opts
	// rsync itself uses /usr/include/popt.h for option parsing
	opt := getoptions.New()

	// rsync (but not openrsync) bundles short options together, i.e. it sends
	// e.g. -logDtpr
	opt.SetMode(getoptions.Bundling)

	opt.Bool("help", false, opt.Alias("h"))

	// gokr-rsyncd flags
	opt.StringVar(&opts.Gokrazy.Listen, "gokr.listen", "", opt.Description("[host]:port listen address for the rsync daemon protocol"))
	opt.StringVar(&opts.Gokrazy.MonitoringListen, "gokr.monitoring_listen", "", opt.Description("optional [host]:port listen address for a HTTP debug interface"))
	opt.StringVar(&opts.Gokrazy.AnonSSHListen, "gokr.anonssh_listen", "", opt.Description("optional [host]:port listen address for the rsync daemon protocol via anonymous SSH"))
	opt.StringVar(&opts.Gokrazy.ModuleMap, "gokr.modulemap", "nonex=/nonexistant/path", opt.Description("<modulename>=<path> pairs for quick setup of the server, without a config file"))

	// rsync-compatible flags
	opt.BoolVar(&opts.Daemon, "daemon", false, opt.Description("run as an rsync daemon"))
	opt.BoolVar(&opts.Server, "server", false)
	opt.BoolVar(&opts.Sender, "sender", false)
	opt.BoolVar(&opts.PreserveGid, "group", false, opt.Alias("g"))
	opt.BoolVar(&opts.PreserveUid, "owner", false, opt.Alias("o"))
	opt.BoolVar(&opts.PreserveLinks, "links", false, opt.Alias("l"))
	// TODO: implement PreservePerms
	opt.BoolVar(&opts.PreservePerms, "perms", false, opt.Alias("p"))
	opt.BoolVar(&opts.D, "D", false)
	opt.BoolVar(&opts.Recurse, "recursive", false, opt.Alias("r"))
	// TODO: implement PreserveTimes
	opt.BoolVar(&opts.PreserveTimes, "times", false, opt.Alias("t"))
	opt.Bool("v", false)     // verbosity; ignored
	opt.Bool("debug", false) // debug; ignored
	// TODO: implement IgnoreTimes
	opt.BoolVar(&opts.IgnoreTimes, "ignore-times", false, opt.Alias("I"))
	opt.BoolVar(&opts.DryRun, "dry-run", false, opt.Alias("n"))

	return &opts, opt
}

func (c *rsyncConn) sendFile(fileIndex int32, fl file) error {
	// rsync/rsync.h defines chunkSize as 32 * 1024, but increasing it to 256K
	// increases throughput with “tridge” rsync as client by 50 Mbit/s.
	const chunkSize = 256 * 1024

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

	// Calculate the md4 hash in a goroutine.
	//
	// This allows an rsync connection to benefit from more than 1 core!
	//
	// We calculate the hash by opening the same file again and reading
	// independently. This keeps the hot loop below focused on shoveling data
	// into the network socket as quickly as possible.
	var eg errgroup.Group
	eg.Go(func() error {
		f, err := os.Open(fl.path)
		if err != nil {
			return err
		}
		defer f.Close()
		var buf [chunkSize]byte
		if _, err := io.CopyBuffer(h, f, buf[:]); err != nil {
			return err
		}
		return nil
	})

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
	}
	// transfer finished:
	if err := c.writeInt32(0); err != nil {
		return err
	}

	// whole file long checksum (16 bytes)
	if err := eg.Wait(); err != nil {
		return err
	}
	sum := h.Sum(nil)
	// log.Printf("sum: %x (len = %d)", sum, len(sum))
	if _, err := c.wr.Write(sum); err != nil {
		return err
	}
	return nil
}

// rsync/rsync.h:struct sum_buf
type sumBuf struct {
	offset int64
	len    int64
	index  int32
	sum1   uint32
	sum2   [16]byte
}

// rsync/sender.c:receive_sums()
func (c *rsyncConn) receiveSums() (sumHead, error) {
	head, err := c.readSumHead()
	if err != nil {
		return sumHead{}, err
	}
	var offset int64
	head.Sums = make([]sumBuf, int(head.ChecksumCount))
	for i := int32(0); i < head.ChecksumCount; i++ {
		shortChecksum, err := c.readInt32()
		if err != nil {
			return sumHead{}, err
		}
		sb := sumBuf{
			index:  i,
			offset: offset,
			sum1:   uint32(shortChecksum),
		}
		if i == head.ChecksumCount-1 && head.RemainderLength != 0 {
			sb.len = int64(head.RemainderLength)
		} else {
			sb.len = int64(head.BlockLength)
		}
		offset += sb.len
		n, err := io.ReadFull(c.rd, sb.sum2[:head.ChecksumLength])
		if err != nil {
			return sumHead{}, err
		}
		_ = n
		// log.Printf("chunk[%d] len=%d offset=%.0f sum1=%08x, sum2=%x",
		// 	i, sb.len, float64(sb.offset), sb.sum1, sb.sum2[:n])
		head.Sums[i] = sb
	}
	return head, nil
}

// rsync/rsync.h defines chunkSize as 32 * 1024, but increasing it to 256K
// increases throughput with “tridge” rsync as client by 50 Mbit/s.
const chunkSize = 256 * 1024

// rsync/match.c:hash_search
func (c *rsyncConn) hashSearch(targets []target, tagTable map[uint16]int, head sumHead, fileIndex int32, fl file) error {
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

	if err := c.writeSumHead(head); err != nil {
		return err
	}

	// sum_init()
	h := md4.New()
	binary.Write(h, binary.LittleEndian, c.seed)

	// The following quotes are citations from
	// https://www.samba.org/~tridge/phd_thesis.pdf, section 3.2.6 The
	// signature search algorithm (PDF page 64).

	// “Once the sorted signature table and the index table have been formed the
	// signature search process can begin. For each byte offset in a_i the fast
	// signature is computed, along with the 16 bit hash of the fast
	// signature. The 16 bit hash is then used to lookup the signature index,
	// giving the index in the signature table of the first fast signature with
	// that hash.”

	var k int
	var sum uint32
	var s1, s2 uint16
	var chunk []byte
	var offset int64
	end := fi.Size() + 1 - head.Sums[len(head.Sums)-1].len
	log.Printf("last block len=%d, end=%d", head.Sums[len(head.Sums)-1].len, end)

	readChunk := func() error {
		k = int(head.BlockLength)
		if remaining := int(fi.Size() - offset); remaining < k {
			k = remaining
		}

		buf := make([]byte, k)
		// TODO: here and below: do we need io.ReadFull?
		n, err := f.ReadAt(buf, offset)
		// log.Printf("reading chunk at offset=%d with len=%d (ret=%d)", offset, len(buf), n)
		if err != nil {
			return err
		}
		chunk = buf[:n]
		sum = getChecksum1(chunk)
		s1 = uint16(sum & 0xFFFF)
		s2 = uint16(sum >> 16)
		return nil
	}
	if err := readChunk(); err != nil {
		return err
	}

	tagHits := 0
Outer:
	for {
		tag := gettag2(s1, s2)
		var sum2 []byte
		doneCsum2 := false
		j, ok := tagTable[tag]
		if ok {
			// “A linear search is then performed through the signature table, stopping
			// when an entry is found with a 16 bit hash which doesn’t match. For each
			// entry the current 32 bit fast signature is compared to the entry in the
			// signature table, and if that matches then the full 128 bit strong
			// signature is computed at the current byte offset and compared to the
			// strong signature in the signature table”
			sum = (uint32(s1) & 0xFFFF) | (uint32(s2) << 16)
			tagHits++
			for ; j < int(head.ChecksumCount) && targets[j].tag == tag; j++ {
				i := targets[j].index
				if sum != head.Sums[i].sum1 {
					continue
				}

				l := int64(head.BlockLength)
				if v := fi.Size() - offset; v < l {
					l = v
				}
				if l != head.Sums[i].len {
					continue
				}

				// log.Printf("potential match at %d target=%d %d sum=%08x", offset, j, i, sum)

				if !doneCsum2 {
					buf := make([]byte, l)
					n, err := f.ReadAt(buf, offset)
					if err != nil {
						return err
					}
					sum2 = getChecksum2(c.seed, buf[:n])
					doneCsum2 = true
				}

				if local, remote := sum2[:head.ChecksumLength], head.Sums[i].sum2[:head.ChecksumLength]; !bytes.Equal(local, remote) {
					log.Printf("false alarm: local %x, remote %x", local, remote)
					//falseAlarms++
					continue
				}

				// TODO(optimization): tridge rsync locates adjacent matches
				// here for better run-length encoding, but I’m not sure where
				// (if at all) we currently use run-length encoding:
				// https://github.com/WayneD/rsync/commit/923fa978088f4c044eec528d9472962d9c9d13c3

				// “If the strong signature is found to match then A emits a
				// token telling B that a match was found and which block in bi
				// was matched12. The search then continues at the byte after
				// the matching block.”

				if err := c.matched(h, f, head, offset, i); err != nil {
					return err
				}

				offset += head.Sums[i].len
				if err := readChunk(); err != nil {
					return fmt.Errorf("readChunk: %v", err)
				}

				if offset >= end {
					break
				}

				continue Outer

				// rsync doesn’t read the next chunk (offset+sums[i].len),
				// rsync starts reading one byte before the next chunk
				// (offset+sums[i].len-1), because the code path starting at
				// “null_tag” removes the chunk’s first byte and adds the
				// next byte after the chunk.
				offset += head.Sums[i].len - 1
				if err := readChunk(); err != nil {
					return fmt.Errorf("readChunk: %v", err)
				}
				break
			}
		}
		// null_tag
		offset++
		if offset >= end {
			break
		}

		if err := readChunk(); err != nil {
			return fmt.Errorf("readChunk: %v", err)
		}

		continue Outer

		// TODO: make the rolling checksum below work:

		//log.Printf("null_tag, k=%d", k)
		readk := k + 1
		add := k < int(fi.Size()-offset)
		if !add {
			readk--
		}

		if readk > 0 {
			buf := make([]byte, readk)
			n, err := f.ReadAt(buf, offset)
			//log.Printf("ReadAt(offset=%d, len=%d, ret=%d)", offset, len(buf), n)
			if err != nil {
				return fmt.Errorf("[resync] ReadAt(%v, len=%d): %v", offset, len(buf), err)
			}
			update := buf[:n]
			s1 -= uint16(update[0])
			s2 -= uint16(k) * uint16(update[0])

			if add {
				s1 += uint16(update[k])
				s2 += s1
			} else {
				log.Printf("WARNING: not enough bytes available")
				k--
			}

			// TODO: match early
			// if err := c.matched(h, f, head, offset-int64(head.BlockLength), -2); err != nil {
			// 	return err
			// }
		}

		offset++
		if offset >= end {
			break
		}
	}

	if err := c.matched(h, f, head, fi.Size(), -1); err != nil {
		return err
	}

	{
		sum := h.Sum(nil)
		log.Printf("sum: %x (len = %d)", sum, len(sum))
		if _, err := c.wr.Write(sum); err != nil {
			return err
		}
	}

	return nil

}

// rsync/token.c:simple_send_token
func (c *rsyncConn) simpleSendToken(f *os.File, token int32, offset int64, n int64) error {
	if n > 0 {
		log.Printf("sending unmatched chunks")
		l := int64(0)
		for l < n {
			n1 := int64(chunkSize)
			if n-l < n1 {
				n1 = n - l
			}

			buf := make([]byte, n1)
			n, err := f.ReadAt(buf, offset+l)
			if err != nil {
				return fmt.Errorf("ReadAt(%v): %v", offset+l, err)
			}
			chunk := buf[:n]

			if err := c.writeInt32(int32(n1)); err != nil {
				return err
			}

			if _, err := c.wr.Write(chunk); err != nil {
				return err
			}

			l += n1
		}
	}
	if token != -2 {
		return c.writeInt32(-(int32(token) + 1))
	}
	return nil
}

// rsync/token.c:send_token
func (c *rsyncConn) sendToken(f *os.File, i int32, offset int64, n int64, toklen int64) error {
	// TODO(compression): send deflated token
	return c.simpleSendToken(f, i, offset, n)
}

type target struct {
	index int32
	tag   uint16
}

func (c *rsyncConn) matched(h hash.Hash, f *os.File, head sumHead, offset int64, i int32) error {
	n := offset - c.lastMatch

	transmitAccumulated := i < 0

	if !transmitAccumulated {
		log.Printf("match at offset=%d last_match=%d i=%d len=%d n=%d",
			offset, c.lastMatch, i, head.Sums[i].len, n)
	}

	l := int64(0)
	if !transmitAccumulated {
		l = head.Sums[i].len
	}

	if err := c.sendToken(f, i, c.lastMatch, n, l); err != nil {
		return fmt.Errorf("sendToken: %v", err)
	}
	// TODO: data_transfer += n;

	if !transmitAccumulated {
		// stats.matched_data += s->sums[i].len;
		n += head.Sums[i].len
	}

	for j := int64(0); j < n; j += chunkSize {
		n1 := int64(chunkSize)
		if n-j < n1 {
			n1 = n - j
		}

		buf := make([]byte, n1)
		n, err := f.ReadAt(buf, c.lastMatch+j)
		if err != nil {
			return fmt.Errorf("ReadAt(%d): %v", c.lastMatch+j, err)
		}
		chunk := buf[:n]
		h.Write(chunk)
	}

	if !transmitAccumulated {
		c.lastMatch = offset + head.Sums[i].len
	} else {
		c.lastMatch = offset
	}
	return nil
}

// rsync/sender.c:send_files()
func (c *rsyncConn) sendFiles(fileList *fileList, dryRun bool) error {
	phase := 0
	for {
		// receive data about receiver’s copy of the file list contents (not
		// ordered)
		// see (*rsync.Receiver).Generator()
		fileIndex, err := c.readInt32()
		if err != nil {
			return err
		}
		if fileIndex == -1 {
			if phase == 0 {
				phase++
				// acknowledge phase change by sending -1
				if err := c.writeInt32(-1); err != nil {
					return err
				}
				continue
			}
			break
		}

		if dryRun {
			if err := c.writeInt32(fileIndex); err != nil {
				return err
			}
			continue
		}

		head, err := c.receiveSums()
		if err != nil {
			return err
		}

		// The following quotes are citations from
		// https://www.samba.org/~tridge/phd_thesis.pdf, section 3.2.6 The
		// signature search algorithm (PDF page 64).

		// rsync/match.c:build_hash_table
		targets := make([]target, len(head.Sums))
		tagTable := make(map[uint16]int) // TODO: or int32 more specifically?
		{
			// “The first step in the algorithm is to sort the received
			// signatures by a 16 bit hash of the fast signature.”
			for idx, sum := range head.Sums {
				targets[idx] = target{
					index: int32(idx),
					tag:   gettag(sum.sum1),
				}
			}
			sort.Slice(targets, func(i, j int) bool {
				return targets[i].tag < targets[j].tag
			})

			// “A 16 bit index table is then formed which takes a 16 bit hash
			// value and gives an index into the sorted signature table which
			// points to the first entry in the table which has a matching
			// hash.”
			for idx := range head.Sums {
				tagTable[targets[idx].tag] = idx
			}
		}

		if len(head.Sums) == 0 {
			// fast path: send the whole file
			err = c.sendFile(fileIndex, fileList.files[fileIndex])
		} else {
			err = c.hashSearch(targets, tagTable, head, fileIndex, fileList.files[fileIndex])
		}
		if err != nil {
			if _, ok := err.(*os.PathError); ok {
				// OpenFile() failed. Log the error (server side only) and
				// proceed. Only starting with protocol 30, an I/O error flag is
				// sent after the file transfer phase.
				if os.IsNotExist(err) {
					log.Printf("file has vanished: %s", fileList.files[fileIndex].path)
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

type countingReader struct {
	r    io.Reader
	read int64
}

func (r *countingReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	r.read += int64(n)
	return n, err
}

type countingWriter struct {
	w       io.Writer
	written int64
}

func (w *countingWriter) Write(p []byte) (n int, err error) {
	n, err = w.w.Write(p)
	w.written += int64(n)
	return n, err
}

func CounterPair(r io.Reader, w io.Writer) (*countingReader, *countingWriter) {
	crd := &countingReader{r: r}
	cwr := &countingWriter{w: w}
	return crd, cwr
}

// protocol version 27 seems to be the safest bet for wide compatibility:
// version 27 was introduced by rsync 2.6.0 (released 2004), and is
// supported by openrsync and rsyn.
const protocolVersion = 27

func (s *Server) HandleDaemonConn(conn io.ReadWriter) (err error) {
	const terminationCommand = "@RSYNCD: OK\n"
	crd := &countingReader{r: conn}
	cwr := &countingWriter{w: conn}
	rd := bufio.NewReader(crd)
	// send server greeting

	fmt.Fprintf(cwr, "@RSYNCD: %d\n", protocolVersion)

	// read client greeting
	clientGreeting, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(clientGreeting, "@RSYNCD: ") {
		return fmt.Errorf("invalid client greeting: got %q", clientGreeting)
	}
	// TODO: protocol negotiation

	// read requested module(s), if any
	requestedModule, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	requestedModule = strings.TrimSpace(requestedModule)
	if requestedModule == "" || requestedModule == "#list" {
		log.Printf("client requested rsync module listing")
		io.WriteString(cwr, s.formatModuleList())
		io.WriteString(cwr, "@RSYNCD: EXIT\n")
		return nil
	}
	log.Printf("client requested rsync module %q", requestedModule)
	module, err := s.getModule(requestedModule)
	if err != nil {
		fmt.Fprintf(cwr, "@ERROR: Unknown module '%s'\n", requestedModule)
		return err
	}

	io.WriteString(cwr, terminationCommand)

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
	opts, opt := NewGetOpt()

	//getoptions.Debug.SetOutput(os.Stderr)
	remaining, err := opt.Parse(flags)
	if err != nil {
		// TODO: terminate connection with an error about which flag is not
		// supported
		return fmt.Errorf("opt.Parse: %v", err)
	}
	if opts.D {
		opts.PreserveDevices = true
		opts.PreserveSpecials = true
	}
	log.Printf("remaining: %q", remaining)
	// remaining[0] is always "."
	// remaining[1] is the first directory
	if len(remaining) < 2 {
		return fmt.Errorf("invalid args: at least one directory required")
	}
	if got, want := remaining[0], "."; got != want {
		return fmt.Errorf("protocol error: got %q, expected %q", got, want)
	}
	paths := remaining[1:]

	// TODO: verify --sender is set and error out otherwise

	return s.HandleConn(module, rd, crd, cwr, paths, opts, false)
}

// handleConn is equivalent to rsync/main.c:start_server
func (s *Server) HandleConn(module config.Module, rd io.Reader, crd *countingReader, cwr *countingWriter, paths []string, opts *Opts, negotiate bool) (err error) {
	// “SHOULD be unique to each connection” as per
	// https://github.com/JohannesBuchner/Jarsync/blob/master/jarsync/rsync.txt
	//
	// TODO: random seed
	// TODO: from which source?
	const sessionChecksumSeed = 666

	c := &rsyncConn{
		seed: sessionChecksumSeed,
		rd:   rd,
		wr:   cwr,
	}

	if negotiate {
		remoteProtocol, err := c.readInt32()
		if err != nil {
			return err
		}
		log.Printf("remote protocol: %d", remoteProtocol)
		if err := c.writeInt32(protocolVersion); err != nil {
			return err
		}
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
	got, err := c.readInt32()
	if err != nil {
		return err
	}
	if want := int32(exclusionListEnd); got != want {
		return fmt.Errorf("protocol error: non-empty exclusion list received")
	}

	log.Printf("exclusion list read")

	// “Update exchange” as per
	// https://github.com/kristapsdz/openrsync/blob/master/rsync.5

	// send file list
	fileList, err := s.sendFileList(c, module, opts, paths)
	if err != nil {
		return err
	}

	log.Printf("file list sent")

	// Sort the file list. The client sorts, so we need to sort, too (in the
	// same way!), otherwise our indices do not match what the client will
	// request.
	sort.Slice(fileList.files, func(i, j int) bool {
		return fileList.files[i].wpath < fileList.files[j].wpath
	})

	if err := c.sendFiles(fileList, opts.DryRun); err != nil {
		return err
	}

	// send statistics:
	// total bytes read (from network connection)
	if err := c.writeInt64(crd.read); err != nil {
		return err
	}
	// total bytes written (to network connection)
	if err := c.writeInt64(cwr.written); err != nil {
		return err
	}
	// total size of files
	if err := c.writeInt64(fileList.totalSize); err != nil {
		return err
	}

	log.Printf("reading final int32")

	finish, err := c.readInt32()
	if err != nil {
		return err
	}
	if finish != -1 {
		return fmt.Errorf("protocol error: expected final -1, got %d", finish)
	}

	log.Printf("HandleConn done")

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
			if err := s.HandleDaemonConn(conn); err != nil {
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
