package rsyncd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/receiver"
	"github.com/gokrazy/rsync/internal/rsyncopts"
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
	crd := &rsyncwire.CountingReader{R: conn}
	cwr := &rsyncwire.CountingWriter{W: conn}
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
	pc, err := rsyncopts.ParseArguments(flags, false)
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
	opts := pc.Options
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

	return s.HandleConn(module, rd, crd, cwr, paths, opts, false)
}

// handleConn is equivalent to rsync/main.c:start_server
func (s *Server) HandleConn(module Module, rd io.Reader, crd *rsyncwire.CountingReader, cwr *rsyncwire.CountingWriter, paths []string, opts *rsyncopts.Options, negotiate bool) (err error) {
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
func (s *Server) handleConnReceiver(module Module, crd *rsyncwire.CountingReader, cwr *rsyncwire.CountingWriter, paths []string, opts *rsyncopts.Options, negotiate bool, c *rsyncwire.Conn, sessionChecksumSeed int32) (err error) {
	s.logger.Printf("handleConnReceiver")

	if !module.Writable {
		return fmt.Errorf("ERROR: module is read only")
	}

	rt := &receiver.Transfer{
		Opts: &receiver.TransferOpts{
			DryRun: opts.DryRun(),

			DeleteMode:       opts.DeleteMode(),
			PreserveGid:      opts.PreserveGid(),
			PreserveUid:      opts.PreserveUid(),
			PreserveLinks:    opts.PreserveLinks(),
			PreservePerms:    opts.PreservePerms(),
			PreserveDevices:  opts.PreserveDevices(),
			PreserveSpecials: opts.PreserveSpecials(),
			PreserveTimes:    opts.PreserveMTimes(),
			// TODO: PreserveHardlinks: opts.PreserveHardlinks,
		},
		Dest: module.Path,
		// TODO: what is Env used for and can we get rid of it?
		Env: receiver.Osenv{
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
		},
		Conn: c,
		Seed: sessionChecksumSeed,
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
	s.logger.Printf("receiving file list")
	fileList, err := rt.ReceiveFileList()
	if err != nil {
		return err
	}
	s.logger.Printf("received %d names", len(fileList))
	stats, err := rt.Do(c, fileList, true)
	if err != nil {
		return err
	}

	s.logger.Printf("stats: %+v", stats)
	return nil
}

// handleConnSender is equivalent to rsync/main.c:do_server_sender
func (s *Server) handleConnSender(module Module, crd *rsyncwire.CountingReader, cwr *rsyncwire.CountingWriter, paths []string, opts *rsyncopts.Options, negotiate bool, c *rsyncwire.Conn, sessionChecksumSeed int32) (err error) {
	st := &sender.Transfer{
		Logger: s.logger,
		Opts:   opts,
		Conn:   c,
		Seed:   sessionChecksumSeed,
	}
	stats, err := st.Do(crd, cwr, module.Name, module.Path, paths)
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
