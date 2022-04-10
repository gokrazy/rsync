package rsyncd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

type sendTransfer struct {
	// config
	logger log.Logger
	opts   *Opts

	// state
	conn      *rsyncwire.Conn
	seed      int32
	lastMatch int64
}

type Module struct {
	Name string   `toml:"name"`
	Path string   `toml:"path"`
	ACL  []string `toml:"acl"`
}

// Option specifies the server options.
type Option interface {
	applyServer(*Server)
}

type serverOptionFunc func(server *Server)

func (f serverOptionFunc) applyServer(s *Server) {
	f(s)
}

// WithLogger specifies the logger to use for the server.
// It also sets the global logger used by the rsync package.
func WithLogger(logger log.Logger) Option {
	return serverOptionFunc(func(s *Server) {
		s.logger = logger

		// TODO: remove global logger usage once we remove
		//       the ad-hoc logger reference.
		log.SetLogger(logger)
	})
}

func NewServer(modules []Module, opts ...Option) (*Server, error) {
	for _, mod := range modules {
		if err := validateModule(mod); err != nil {
			return nil, err
		}
	}

	server := &Server{
		logger:  log.Default(),
		modules: modules,
	}

	for _, opt := range opts {
		opt.applyServer(server)
	}

	return server, nil
}

type Server struct {
	logger log.Logger

	modules []Module
}

func (s *Server) getModule(requestedModule string) (Module, error) {
	for _, mod := range s.modules {
		if mod.Name == requestedModule {
			return mod, nil
		}
	}

	return Module{}, fmt.Errorf("no such module: %s", requestedModule)
}

func (s *Server) formatModuleList() string {
	if len(s.modules) == 0 {
		return ""
	}
	var list strings.Builder
	for _, mod := range s.modules {
		comment := mod.Name // for now
		fmt.Fprintf(&list, "%s\t%s\n",
			mod.Name,
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

func checkACL(acls []string, remoteAddr net.Addr) error {
	if len(acls) == 0 {
		return nil
	}
	host, _, err := net.SplitHostPort(remoteAddr.String())
	if err != nil {
		return fmt.Errorf("BUG: invalid remote address %q", remoteAddr.String())
	}
	remoteIP := net.ParseIP(host)
	if remoteIP == nil {
		return fmt.Errorf("BUG: invalid remote host %q", host)
	}
	for _, acl := range acls {
		// TODO(performance): move ACL parsing to config-time to make ACL checks
		// less expensive
		i := strings.Index(acl, " ")
		if i < 0 {
			return fmt.Errorf("invalid acl: %q (no space found)", acl)
		}
		action, who := acl[:i], acl[i+len(" "):]
		if action != "allow" && action != "deny" {
			return fmt.Errorf("invalid acl: %q (syntax: allow|deny <all|ipnet>)", acl)
		}
		if who == "all" {
			// The all keyword matches any remote IP address
		} else {
			_, net, err := net.ParseCIDR(who)
			if err != nil {
				return fmt.Errorf("invalid acl: %q (syntax: allow|deny <all|ipnet>)", acl)
			}
			if !net.Contains(remoteIP) {
				// Skip this instruction, the remote IP does not match
				continue
			}
		}
		switch action {
		case "allow":
			return nil
		case "deny":
			return fmt.Errorf("access denied (acl %q)", acl)
		default:
			return fmt.Errorf("invalid acl: %q (syntax: allow|deny <all|ipnet>)", acl)
		}
	}
	return nil
}

// FIXME: context cancellation not yet implemented
func (s *Server) HandleDaemonConn(ctx context.Context, conn io.ReadWriter, remoteAddr net.Addr) (err error) {
	_ = ctx // not implemented. what would be the best thing to do? wrap conn's reader part with cancelable reader?

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
		s.logger.Printf("client %v requested rsync module listing", remoteAddr)
		io.WriteString(cwr, s.formatModuleList())
		io.WriteString(cwr, "@RSYNCD: EXIT\n")
		return nil
	}
	s.logger.Printf("client %v requested rsync module %q", remoteAddr, requestedModule)
	module, err := s.getModule(requestedModule)
	if err != nil {
		fmt.Fprintf(cwr, "@ERROR: Unknown module %q\n", requestedModule)
		return err
	}

	if err := checkACL(module.ACL, remoteAddr); err != nil {
		fmt.Fprintf(cwr, "@ERROR: %v\n", err)
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
		s.logger.Printf("client sent: %q", flag)
		if flag == "" {
			break
		}
		flags = append(flags, flag)
	}

	s.logger.Printf("flags: %+v", flags)
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
	s.logger.Printf("remaining: %q", remaining)
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
func (s *Server) HandleConn(module Module, rd io.Reader, crd *countingReader, cwr *countingWriter, paths []string, opts *Opts, negotiate bool) (err error) {
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
		s.logger.Printf("remote protocol: %d", remoteProtocol)
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
		logger: s.logger,
		opts:   opts,
		conn:   c,
		seed:   sessionChecksumSeed,
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

	s.logger.Printf("exclusion list read")

	// “Update exchange” as per
	// https://github.com/kristapsdz/openrsync/blob/master/rsync.5

	// send file list
	fileList, err := st.sendFileList(module, opts, paths)
	if err != nil {
		return err
	}

	s.logger.Printf("file list sent")

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

	s.logger.Printf("reading final int32")

	finish, err := c.ReadInt32()
	if err != nil {
		return err
	}
	if finish != -1 {
		return fmt.Errorf("protocol error: expected final -1, got %d", finish)
	}

	s.logger.Printf("HandleConn done")

	return nil
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close() // unblocks Accept()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil // ignore expected 'use of closed network connection' error on context cancel
			default:
				return err
			}
		}
		remoteAddr := conn.RemoteAddr()
		s.logger.Printf("remote connection from %s", remoteAddr)
		go func() {
			defer conn.Close()
			if err := s.HandleDaemonConn(ctx, conn, remoteAddr); err != nil {
				s.logger.Printf("[%s] handle: %v", remoteAddr, err)
			}
		}()
	}
}

func validateModule(mod Module) error {
	if mod.Name == "" {
		return errors.New("module has no name")
	}
	if mod.Path == "" {
		return fmt.Errorf("module %q has empty path", mod.Name)
	}

	return nil
}
