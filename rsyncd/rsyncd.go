// Package rsyncd implements an rsync server (only), but note that gokrazy/rsync
// contains a native Go rsync implementation that supports sending and receiving
// files as client or server, compatible with the original tridge rsync (from
// the samba project) or openrsync (used on OpenBSD and macOS 15+).
package rsyncd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/progress"
	"github.com/gokrazy/rsync/internal/receiver"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncwire"
	"github.com/gokrazy/rsync/internal/sender"
)

type Module struct {
	Name     string   `toml:"name"`
	Path     string   `toml:"path"`
	ACL      []string `toml:"acl"`
	Writable bool     `toml:"writable"`
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
	})
}

func WithStderr(stderr io.Writer) Option {
	return serverOptionFunc(func(s *Server) {
		s.stderr = stderr
	})
}

func DontRestrict() Option {
	return serverOptionFunc(func(s *Server) {
		s.dontRestrict = true
	})
}

func NewServer(modules []Module, opts ...Option) (*Server, error) {
	for _, mod := range modules {
		if err := validateModule(mod); err != nil {
			return nil, err
		}
	}

	server := &Server{
		modules: modules,
	}

	for _, opt := range opts {
		opt.applyServer(server)
	}

	// Default to os.Stderr if no stderr was specified.
	// Explicitly use io.Discard if you do not want stderr.
	if server.stderr == nil {
		server.stderr = os.Stderr
	}

	if server.logger == nil {
		// TODO: use the logger in a *rsyncos.Env instead
		server.logger = log.New(server.stderr)
	}

	// An empty module list means this server is a sender
	// (e.g. started in command mode with --server --sender),
	// in which case restrict.MaybeFileSystem() will be called
	// by the caller of NewServer().
	if !server.dontRestrict && len(server.modules) > 0 {
		if err := restrictToModules(server.modules); err != nil {
			return nil, err
		}
	}

	return server, nil
}

type Server struct {
	stderr       io.Writer
	logger       log.Logger
	dontRestrict bool

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

func checkACL(acls []string, remoteAddr string) error {
	if len(acls) == 0 {
		return nil
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return fmt.Errorf("BUG: invalid remote address %q", remoteAddr)
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
func (s *Server) HandleDaemonConn(ctx context.Context, conn *Conn) (err error) {
	_ = ctx // not implemented. what would be the best thing to do? wrap conn's reader part with cancelable reader?

	const terminationCommand = "@RSYNCD: OK\n"
	cwr := conn.cwr
	rd := conn.rd
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
		s.logger.Printf("client %v requested rsync module listing", conn.name)
		io.WriteString(cwr, s.formatModuleList())
		io.WriteString(cwr, "@RSYNCD: EXIT\n")
		return nil
	}
	s.logger.Printf("client %v requested rsync module %q", conn.name, requestedModule)
	module, err := s.getModule(requestedModule)
	if err != nil {
		fmt.Fprintf(cwr, "@ERROR: Unknown module %q\n", requestedModule)
		return err
	}

	if err := checkACL(module.ACL, conn.name); err != nil {
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
	osenv := &rsyncos.Env{Stderr: s.stderr}
	pc := rsyncopts.NewContext(rsyncopts.NewOptionsWithGokrazyDefaults(osenv))
	if err := pc.ParseArguments(osenv, flags); err != nil {
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
		mpx.WriteMsg(rsyncwire.MsgError, fmt.Appendf(nil, "gokr-rsync [sender]: %v\n", err))

		return err
	}
	remaining := pc.RemainingArgs
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
	s.logger.Printf("paths: %q", paths)

	// Strip the module_name/ prefix out of the paths,
	// see rsync/io.c:read_args, glob_expand_module().
	for idx, path := range pc.RemainingArgs {
		if idx == 0 {
			// skip pc.RemainingArgs[0], only strip RemainingArgs[1:]
			continue
		}
		trimmed := strings.TrimPrefix(path, module.Name)
		if trimmed == "" {
			trimmed = "."
		}
		pc.RemainingArgs[idx] = trimmed
	}

	s.logger.Printf("trimmed paths: %q", pc.RemainingArgs[1:])

	return s.handleConn(ctx, conn, &module, pc, false)
}

type Conn struct {
	name string
	crd  *rsyncwire.CountingReader
	cwr  *rsyncwire.CountingWriter
	rd   *bufio.Reader
}

func NewConnection(r io.Reader, w io.Writer, name string) *Conn {
	crd, cwr := rsyncwire.CounterPair(r, w)
	rd := bufio.NewReader(crd)
	return &Conn{
		name: name,
		crd:  crd,
		cwr:  cwr,
		rd:   rd,
	}
}

// This method is only exported until we refactor; use HandleConnArgs() instead
func (s *Server) InternalHandleConn(ctx context.Context, conn *Conn, module *Module, pc *rsyncopts.Context) error {
	return s.handleConn(ctx, conn, module, pc, true /* negotiate */)
}

func (s *Server) HandleConnArgs(ctx context.Context, conn *Conn, module *Module, args []string) error {
	osenv := &rsyncos.Env{Stderr: s.stderr}
	pc := rsyncopts.NewContext(rsyncopts.NewOptionsWithGokrazyDefaults(osenv))
	if err := pc.ParseArguments(osenv, args); err != nil {
		return fmt.Errorf("parsing server args: %v", err)
	}
	return s.handleConn(ctx, conn, module, pc, true /* negotiate */)
}

// handleConn is equivalent to rsync/main.c:start_server
func (s *Server) handleConn(ctx context.Context, conn *Conn, module *Module, pc *rsyncopts.Context, negotiate bool) (err error) {
	rd := conn.rd
	crd := conn.crd
	cwr := conn.cwr
	opts := pc.Options
	paths := pc.RemainingArgs[1:]

	// “SHOULD be unique to each connection” as per
	// https://github.com/JohannesBuchner/Jarsync/blob/master/jarsync/rsync.txt
	//
	// Computed the same way that tridge rsync does it, but the details do not
	// matter. The goal is to have a checksum seed each time.
	sessionChecksumSeed := int32(time.Now().Unix()) ^ (int32(os.Getpid()) << 6)

	c := &rsyncwire.Conn{
		Reader: rd,
		Writer: cwr,
	}

	if negotiate {
		remoteProtocol, err := c.ReadInt32()
		if err != nil {
			return err
		}
		if opts.DebugGTE(rsyncopts.DEBUG_PROTO, 1) {
			s.logger.Printf("remote protocol: %d", remoteProtocol)
		}
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
	// Update cwr to track the multiplexed writer,
	// but copy the number of bytes written.
	cwr = &rsyncwire.CountingWriter{
		W:            mpx,
		BytesWritten: cwr.BytesWritten,
	}
	c.Writer = cwr

	if opts.Sender() {
		// If returning an error, send the error to the client for display, too:
		defer func() {
			if err != nil {
				mpx.WriteMsg(rsyncwire.MsgError, fmt.Appendf(nil, "gokr-rsync [sender]: %v\n", err))
			}
		}()

		return s.handleConnSender(module, crd, cwr, paths, opts, false, c, sessionChecksumSeed)
	}

	// If returning an error, send the error to the client for display, too:
	defer func() {
		if err != nil {
			mpx.WriteMsg(rsyncwire.MsgError, fmt.Appendf(nil, "gokr-rsync [receiver]: %v\n", err))
		}
	}()
	return s.handleConnReceiver(module, crd, cwr, paths, opts, false, c, sessionChecksumSeed)
}

// handleConnReceiver is equivalent to rsync/main.c:do_server_recv
func (s *Server) handleConnReceiver(module *Module, crd *rsyncwire.CountingReader, cwr *rsyncwire.CountingWriter, paths []string, opts *rsyncopts.Options, negotiate bool, c *rsyncwire.Conn, sessionChecksumSeed int32) (err error) {
	var destPath string
	implicitModule := module == nil
	if implicitModule {
		if len(paths) != 1 {
			return fmt.Errorf("precisely one destination path required, got %q", paths)
		}
		module = &Module{
			Name:     "implicit",
			Path:     paths[0],
			Writable: true,
		}
		destPath = module.Path
	}
	if opts.Verbose() {
		s.logger.Printf("handleConnReceiver(module=%+v, destPath=%q)", module, destPath)
	}

	if !module.Writable {
		return fmt.Errorf("ERROR: module is read only")
	}

	rt := &receiver.Transfer{
		Logger: s.logger,
		Opts: &receiver.TransferOpts{
			DryRun:   opts.DryRun(),
			Server:   opts.Server(),
			Verbose:  opts.Verbose(),
			Progress: opts.Progress(),

			DeleteMode:       opts.DeleteMode(),
			PreserveGid:      opts.PreserveGid(),
			PreserveUid:      opts.PreserveUid(),
			PreserveLinks:    opts.PreserveLinks(),
			PreservePerms:    opts.PreservePerms(),
			PreserveDevices:  opts.PreserveDevices(),
			PreserveSpecials: opts.PreserveSpecials(),
			PreserveTimes:    opts.PreserveMTimes(),
			// TODO: PreserveHardlinks: opts.PreserveHardlinks,
			IgnoreTimes: opts.IgnoreTimes(),

			InfoGTE:  opts.InfoGTE,
			DebugGTE: opts.DebugGTE,
		},
		Dest: module.Path,
		Env: &rsyncos.Env{
			Stderr: s.stderr,
		},
		Conn:     c,
		Seed:     sessionChecksumSeed,
		Progress: progress.NewPrinter(io.Discard, time.Now),
	}
	if err := os.MkdirAll(rt.Dest, 0755); err != nil {
		return fmt.Errorf("MkdirAll(dest=%s): %v", rt.Dest, err)
	}
	rt.DestRoot, err = os.OpenRoot(rt.Dest)
	if err != nil {
		return fmt.Errorf("OpenRoot(dest=%s): %v", rt.Dest, err)
	}
	defer rt.DestRoot.Close()

	if !implicitModule {
		if len(paths) > 1 {
			return fmt.Errorf("module is available, and at most one destination path is allowed, got %q", paths)
		}
		// Descend into subdirectory (if requested),
		// using the os.OpenRoot traversal-safe API.
		if len(paths) == 1 && paths[0] != "/" {
			subdir := strings.TrimPrefix(paths[0], "/")
			subRoot, err := rt.DestRoot.OpenRoot(subdir)
			if err != nil {
				if os.IsNotExist(err) {
					if err := mkdirAll(rt.DestRoot, subdir, 0755); err != nil {
						return fmt.Errorf("MkdirAll(%s): %v", subdir, err)
					}
					subRoot, err = rt.DestRoot.OpenRoot(subdir)
				}
				if err != nil {
					return fmt.Errorf("OpenRoot(%s): %v", subdir, err)
				}
			}
			if name := subRoot.Name(); filepath.IsAbs(name) {
				rt.Dest = name
			} else {
				// Go changed behavior: In Go 1.25, subRoot.Name()
				// did not return an absolute path:
				// https://go.googlesource.com/go/+/ed7f804
				rt.Dest = filepath.Join(rt.Dest, name)
			}
			rt.DestRoot = subRoot
			if opts.Verbose() {
				s.logger.Printf("opened subdirectory %q", rt.Dest)
			}
		}
	}

	if opts.PreserveHardLinks() {
		return fmt.Errorf("support for hard links not yet implemented")
	}

	if opts.DeleteMode() {
		// receive the exclusion list (openrsync’s is always empty)
		exclusionList, err := sender.RecvFilterList(c)
		if err != nil {
			return err
		}
		s.logger.Printf("exclusion list read (entries: %d)", len(exclusionList.Filters))
	}

	// receive file list
	if opts.InfoGTE(rsyncopts.INFO_FLIST, 1) {
		s.logger.Printf("receiving file list")
	}
	fileList, err := rt.ReceiveFileList()
	if err != nil {
		return err
	}
	if opts.InfoGTE(rsyncopts.INFO_FLIST, 1) {
		s.logger.Printf("received %d names", len(fileList))
	}
	stats, err := rt.Do(c, fileList, true)
	if err != nil {
		return err
	}
	if opts.InfoGTE(rsyncopts.INFO_STATS, 1) {
		s.logger.Printf("stats: %+v", stats)
	}
	return nil
}

// handleConnSender is equivalent to rsync/main.c:do_server_sender
func (s *Server) handleConnSender(module *Module, crd *rsyncwire.CountingReader, cwr *rsyncwire.CountingWriter, paths []string, opts *rsyncopts.Options, negotiate bool, c *rsyncwire.Conn, sessionChecksumSeed int32) (err error) {
	if module == nil {
		module = &Module{
			Name: "implicit",
			Path: "/",
		}
	}

	st := &sender.Transfer{
		Logger: s.logger,
		Opts:   opts,
		Conn:   c,
		Seed:   sessionChecksumSeed,
		Env: &rsyncos.Env{
			Stderr: s.stderr,
		},
		Progress: progress.NewPrinter(io.Discard, time.Now),
	}
	// receive the exclusion list (openrsync’s is always empty)
	exclusionList, err := sender.RecvFilterList(st.Conn)
	if err != nil {
		return err
	}
	st.Logger.Printf("exclusion list read (entries: %d)", len(exclusionList.Filters))

	stats, err := st.Do(crd, cwr, module.Path, paths, exclusionList)
	if err != nil {
		return err
	}

	s.logger.Printf("handleConnSender done. stats: %+v", stats)

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
			c := NewConnection(conn, conn, remoteAddr.String())
			if err := s.HandleDaemonConn(ctx, c); err != nil {
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
