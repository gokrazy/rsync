package rsyncd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"sort"
	"strings"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/config"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

type sendTransfer struct {
	// config
	opts *Opts

	// state
	conn      *rsyncwire.Conn
	seed      int32
	lastMatch int64
}

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

// rsync/rsync.h:struct sum_buf
type sumBuf struct {
	offset int64
	len    int64
	index  int32
	sum1   uint32
	sum2   [16]byte
}

// rsync/rsync.h defines chunkSize as 32 * 1024, but increasing it to 256K
// increases throughput with “tridge” rsync as client by 50 Mbit/s.
const chunkSize = 256 * 1024

type target struct {
	index int32
	tag   uint16
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

func (s *Server) HandleDaemonConn(conn io.ReadWriter) (err error) {
	const terminationCommand = "@RSYNCD: OK\n"
	crd := &countingReader{r: conn}
	cwr := &countingWriter{w: conn}
	rd := bufio.NewReader(crd)
	// send server greeting

	fmt.Fprintf(cwr, "@RSYNCD: %d\n", rsync.ProtocolVersion)

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
		err = fmt.Errorf("parsing server args: %v", err)

		// terminate connection with an error about which flag is not supported
		c := &rsyncwire.Conn{
			Reader: rd,
			Writer: cwr,
		}

		const errorSeed = 0xee
		if err := c.WriteInt32(errorSeed); err != nil {
			return err
		}

		// Switch to multiplexing protocol, but only for server-side transmissions.
		// Transmissions received from the client are not multiplexed.
		mpx := &rsyncwire.MultiplexWriter{Writer: c.Writer}
		mpx.WriteMsg(rsyncwire.MsgError, []byte(fmt.Sprintf("gokr-rsync [sender]: %v\n", err)))

		return err
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
	// TODO: random seed. tridge rsync uses time(NULL) ^ (getpid() << 6)
	const sessionChecksumSeed = 666

	c := &rsyncwire.Conn{
		Reader: rd,
		Writer: cwr,
	}

	if negotiate {
		remoteProtocol, err := c.ReadInt32()
		if err != nil {
			return err
		}
		log.Printf("remote protocol: %d", remoteProtocol)
		if err := c.WriteInt32(rsync.ProtocolVersion); err != nil {
			return err
		}
	}

	if err := c.WriteInt32(sessionChecksumSeed); err != nil {
		return err
	}

	// Switch to multiplexing protocol, but only for server-side transmissions.
	// Transmissions received from the client are not multiplexed.
	mpx := &rsyncwire.MultiplexWriter{Writer: c.Writer}
	c.Writer = mpx
	// If returning an error, send the error to the client for display, too:
	defer func() {
		if err != nil {
			mpx.WriteMsg(rsyncwire.MsgError, []byte(fmt.Sprintf("gokr-rsync [sender]: %v\n", err)))
		}
	}()

	st := &sendTransfer{
		opts: opts,
		conn: c,
		seed: sessionChecksumSeed,
	}

	// receive the exclusion list (openrsync’s is always empty)
	const exclusionListEnd = 0
	got, err := c.ReadInt32()
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
	fileList, err := st.sendFileList(c, module, opts, paths)
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

	if err := st.sendFiles(fileList); err != nil {
		return err
	}

	// send statistics:
	// total bytes read (from network connection)
	if err := c.WriteInt64(crd.read); err != nil {
		return err
	}
	// total bytes written (to network connection)
	if err := c.WriteInt64(cwr.written); err != nil {
		return err
	}
	// total size of files
	if err := c.WriteInt64(fileList.totalSize); err != nil {
		return err
	}

	log.Printf("reading final int32")

	finish, err := c.ReadInt32()
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
func sumSizesSqroot(len int64) rsync.SumHead {
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

	return rsync.SumHead{
		ChecksumCount:   int32((len + (int64(blockLength) - 1)) / int64(blockLength)),
		RemainderLength: int32(len % int64(blockLength)),
		BlockLength:     blockLength,
		ChecksumLength:  checksumLength,
	}
}
