package rsyncd

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/DavidGamba/go-getoptions"
	"github.com/kaiakz/rsync-os/rsync"
)

type Server struct {
}

type multiplexWriter struct {
	underlying io.Writer
}

func (w *multiplexWriter) Write(p []byte) (n int, err error) {
	header := uint32(7)<<24 | uint32(len(p))
	log.Printf("len %d (hex %x)", len(p), uint32(len(p)))
	log.Printf("header=%v (%x)", header, header)
	if err := binary.Write(w.underlying, binary.LittleEndian, header); err != nil {
		return 0, err
	}
	return w.underlying.Write(p)
}

type rsyncConn struct {
	wr io.Writer
	rd io.Reader
}

func (c *rsyncConn) writeByte(data byte) error {
	return binary.Write(c.wr, binary.LittleEndian, data)
}

func (c *rsyncConn) writeInt32(data int32) error {
	return binary.Write(c.wr, binary.LittleEndian, data)
}

func (c *rsyncConn) writeInt64(data int64) error {
	// send as a 32-bit integer if possible
	if data <= 0x7FFFFFFF && data >= 0 {
		return c.writeInt32(int32(data))
	}
	// otherwise, send -1 followed by the 64-bit integer
	if err := c.writeInt32(-1); err != nil {
		return err
	}
	return binary.Write(c.wr, binary.LittleEndian, data)
}

func (c *rsyncConn) writeString(data string) error {
	_, err := io.WriteString(c.wr, data)
	return err
}

func (c *rsyncConn) readByte() (byte, error) {
	var buf [1]byte
	if _, err := io.ReadFull(c.rd, buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func (c *rsyncConn) readInt32() (int32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(c.rd, buf[:]); err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(buf[:])), nil
}

func (s *Server) sendFileList(c *rsyncConn, opts rsyncOpts) error {
	var fileEnt bytes.Buffer
	fec := &rsyncConn{wr: &fileEnt}

	//root := "/srv/repo.distr1.org/distri"
	root := "/home/michael/i3/docs"
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		log.Printf("filepath.WalkFn(path=%s)", path)
		if err != nil {
			return err
		}

		// Only ever transmit long names, like openrsync
		flags := byte(rsync.FLIST_NAME_LONG)

		name := strings.TrimPrefix(path, root+"/")
		if path == root {
			name = "."
			flags |= rsync.FLIST_TOP_LEVEL
		}
		log.Printf("flags for %s: %v", name, flags)

		// 1.   status byte (integer)
		if err := fec.writeByte(flags); err != nil {
			return err
		}

		// 2.   inherited filename length (optional, byte)
		// 3.   filename length (integer or byte)
		if err := fec.writeInt32(int32(len(name))); err != nil {
			return err
		}

		// 4.   file (byte array)
		if err := fec.writeString(name); err != nil {
			return err
		}

		// 5.   file length (long)
		sz := info.Size()
		log.Printf("size: %d", sz)
		if err := fec.writeInt64(info.Size()); err != nil {
			return err
		}

		// 6.   file modification time (optional, integer)
		mtime := int32(info.ModTime().Unix())
		log.Printf("mtime = %v", mtime)
		if err := fec.writeInt32(mtime); err != nil {
			return err
		}

		// 7.   file mode (optional, mode_t, integer)
		if err := fec.writeInt32(int32(info.Mode())); err != nil {
			return err
		}

		if opts.PreserveUid {
			// 8.   if -o, the user id (integer)
			if err := fec.writeInt32(1000); err != nil {
				return err
			}
		}

		if opts.PreserveGid {
			// 9.   if -g, the group id (integer)
			if err := fec.writeInt32(1000); err != nil {
				return err
			}
		}

		const isSpecial = false // TODO
		if opts.PreserveSpecials && isSpecial {
			// 10.  if a special file and -D, the device “rdev” type (integer)
			// TODO: what to send here?
			if err := fec.writeInt32(0); err != nil {
				return err
			}
		}

		// 11.  if a symbolic link and -l, the link target's length (integer)
		// 12.  if a symbolic link and -l, the link target (byte array)

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
	log.Printf("filepath.Walk = %v", err)
	if err != nil {
		return err
	}

	const endOfFileList = 0
	if err := fec.writeByte(endOfFileList); err != nil {
		return err
	}

	for i := 0; i < 2; i++ {
		if err := fec.writeInt32(1000); err != nil {
			return err
		}
		if err := fec.writeByte(byte(len("michael"))); err != nil {
			return err
		}
		if err := fec.writeString("michael"); err != nil {
			return err
		}
		const endOfSet = 0
		if err := fec.writeInt32(endOfSet); err != nil {
			return err
		}
	}

	const ioErrors = 0
	if err := fec.writeInt32(ioErrors); err != nil {
		return err
	}

	log.Printf("fileEnt: %x", fileEnt.Bytes())
	// 0000   4b 00 00 07 01 01 2e 3c 00 00 00 8b b9 1a 61 ed   K......<......a.
	//                    ^^ xflags
	//                       ^^ len(name)
	//                          ^^ name = .
	//                             ^^ ^^ ^^ ^^ ^^ ^^ ^^ ^^ size (+extra)
	//                                                     ^^
	// 0010   41 00 00 e8 03 00 00 e8 03 00 00 98 05 64 75 6d   A............dum
	//        ^^ ^^ ^^ mod time
	//                 ^^ ^^ ^^ ^^ uid
	//                             ^^ ^^ ^^ ^^ gid
	//                                         ^^ xflags
	//                                            ^^ len(name)
	// 0020   6d 79 04 00 00 00 a4 81 00 00 00 e8 03 00 00 07   my..............
	//                                         ^^ ^^ ^^ ^^ uid
	//                                                     ^^ len(username)
	// 0030   6d 69 63 68 61 65 6c 00 00 00 00 e8 03 00 00 07   michael.........
	//        ^^ ^^ ^^ ^^ ^^ ^^ ^^ user name
	//                             ^^ ^^ ^^ ^^ end of set
	// 0040   6d 69 63 68 61 65 6c 00 00 00 00 00 00 00 00      michael........
	//                                         ^^ ^^ ^^ ^^ i/o errors

	if err := c.writeString(fileEnt.String()); err != nil {
		return err
	}

	return nil
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
}

func (s *Server) handleConn(conn net.Conn) error {
	const terminationCommand = "@RSYNCD: OK\n"
	rd := bufio.NewReader(conn)
	// send server greeting
	const protocolVersion = "27" // TODO: which is which?
	fmt.Fprintf(conn, "@RSYNCD: %s\n", protocolVersion)

	// read client greeting
	clientGreeting, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(clientGreeting, "@RSYNCD: ") {
		return fmt.Errorf("invalid client greeting: got %q", clientGreeting)
	}
	io.WriteString(conn, terminationCommand)

	// read requested module(s), if any
	requestedModule, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	requestedModule = strings.TrimSpace(requestedModule)
	log.Printf("client sent: %q", requestedModule)
	if requestedModule == "" || requestedModule == "#list" {
		// send available modules
	}
	// TODO: check if requested module exists
	//io.WriteString(conn, "\n")

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
	opt.BoolVar(&opts.Server, "server", false)
	opt.BoolVar(&opts.Sender, "sender", false)
	opt.BoolVar(&opts.PreserveGid, "group", false, opt.Alias("g"))
	opt.BoolVar(&opts.PreserveUid, "owner", false, opt.Alias("o"))
	opt.BoolVar(&opts.PreserveLinks, "links", false, opt.Alias("l"))
	opt.BoolVar(&opts.PreservePerms, "perms", false, opt.Alias("p"))
	dOpt := opt.Bool("D", false)
	opt.BoolVar(&opts.Recurse, "recursive", false, opt.Alias("r"))
	opt.BoolVar(&opts.PreserveTimes, "times", false, opt.Alias("t"))
	opt.Bool("v", false) // verbosity; ignored
	remaining, err := opt.Parse(flags)
	if err != nil {
		// TODO: terminate connection with an error about which flag is not
		// supported
		return err
	}
	if *dOpt {
		opts.PreserveDevices = true
		opts.PreserveSpecials = true
	}
	log.Printf("remaining: %q", remaining)
	// TODO: verify --sender is set and error out otherwise

	// TODO: seed?
	c := &rsyncConn{
		rd: rd,
		wr: conn,
	}

	// “SHOULD be unique to each connection” as per
	// https://github.com/JohannesBuchner/Jarsync/blob/master/jarsync/rsync.txt
	//
	// TODO: random seed
	// TODO: from which source?
	const sessionChecksumSeed = 666
	if err := c.writeInt32(sessionChecksumSeed); err != nil {
		return err
	}

	log.Printf("wrote seed")

	// Switch to multiplexing protocol, but only for server-side transmissions.
	// Transmissions received from the client are not multiplexed.
	c.wr = &multiplexWriter{underlying: c.wr}

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
	if err := s.sendFileList(c, opts); err != nil {
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

	// TODO: receive data about receiver’s copy of the file list contents (not
	// ordered)
	// see (*rsync.Receiver).Generator()
	fileIndex, err := c.readInt32()
	if err != nil {
		return err
	}
	log.Printf("fileIndex: %v (hex %x)", fileIndex, fileIndex)

	// TODO: FileDownloader

	// 4 byte header (first byte: tag, rest: payload length)
	// arbitrary length payload

	// tag 7: normal data
	// tag 1: error on the sender’s part, triggers an exit
	// anything else: out-of-band server messages

	// for {
	// 	// TODO: limit this read?
	// 	payload, err := ioutil.ReadAll(muxr)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	log.Printf("payload: %q", payload)
	// }

	return fmt.Errorf("NYI")
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
