package maincmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/receiver"
	"github.com/gokrazy/rsync/internal/restrict"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncstats"
	"github.com/gokrazy/rsync/internal/rsyncwire"
	"github.com/gokrazy/rsync/internal/sender"
	"github.com/google/shlex"
)

// rsync/main.c:start_client
func rsyncMain(ctx context.Context, osenv *rsyncos.Env, opts *rsyncopts.Options, sources []string, dest string) (*rsyncstats.TransferStats, error) {
	if opts.Verbose() {
		osenv.Logf("dest: %q, sources: %q", dest, sources)
		osenv.Logf("opts: %+v", opts)
	}
	// Guaranteed to be non-empty by caller of rsyncMain().
	src := sources[0]

	if opts.Verbose() {
		osenv.Logf("processing src=%s", src)
	}
	daemonConnection := 0 // no daemon
	host, path, port, err := checkForHostspec(src)
	if opts.Verbose() {
		osenv.Logf("host=%q, path=%q, port=%d, err=%v", host, path, port, err)
	}
	if err != nil {
		// source is local, check dest arg
		opts.SetSender()
		// TODO: remote_argv == "."?
		host, path, port, err = checkForHostspec(dest)
		if opts.Verbose() {
			osenv.Logf("host=%q, path=%q, port=%d, err=%v", host, path, port, err)
		}
		if path == "" {
			if opts.Verbose() {
				osenv.Logf("source and dest are both local!")
			}
			host = ""
			port = 0
			path = dest
			opts.SetLocalServer()
		} else {
			// dest is remote
			if port != 0 {
				if opts.ShellCommand() != "" {
					daemonConnection = 1 // daemon via remote shell
				} else {
					daemonConnection = -1 // daemon via socket
				}
			}
		}
	} else {
		// source is remote
		if port != 0 {
			if opts.ShellCommand() != "" {
				daemonConnection = 1 // daemon via remote shell
			} else {
				daemonConnection = -1 // daemon via socket
			}
		}
	}

	// TODO: if opts.AmSender(), verify extra source args have no hostspec
	var roDirs, rwDirs []string
	other := dest
	if other != "" {
		if !opts.Sender() || opts.LocalServer() {
			// dest is local
			if err := os.MkdirAll(other, 0755); err != nil {
				return nil, err
			}
		}
	}
	paths := []string{other}
	if opts.Sender() {
		// source is local
		other = src
		paths = sources
		roDirs = sources
		if opts.LocalServer() {
			// source and dest are both local
			rwDirs = []string{dest}
		}
	} else {
		if other != "" {
			rwDirs = paths
		}
	}
	if osenv.Restrict() {
		if err := restrict.MaybeFileSystem(roDirs, rwDirs); err != nil {
			return nil, err
		}
	}

	module := path
	if idx := strings.IndexByte(module, '/'); idx > -1 {
		module = module[:idx]
	}
	if opts.Verbose() {
		osenv.Logf("module=%q, path=%q, other=%q", module, path, other)
	}

	if daemonConnection < 0 {
		stats, err := socketClient(ctx, osenv, opts, host, path, port, paths)
		if err != nil {
			return nil, err
		}
		return stats, nil
	}

	machine := host
	user := ""
	if idx := strings.IndexByte(machine, '@'); idx > -1 {
		user = machine[:idx]
		machine = machine[idx+1:]
	}
	rc, wc, err := doCmd(osenv, opts, machine, user, path, daemonConnection)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	defer wc.Close()
	conn := &readWriter{
		r: rc,
		w: wc,
	}
	negotiate := true
	if daemonConnection != 0 {
		done, err := startInbandExchange(osenv, opts, conn, module, path)
		if err != nil {
			return nil, err
		}
		if done {
			return nil, nil
		}
		negotiate = false // already done
	}
	stats, err := ClientRun(osenv, opts, conn, paths, negotiate)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// rsync/main.c:do_cmd
func doCmd(osenv *rsyncos.Env, opts *rsyncopts.Options, machine, user, path string, daemonConnection int) (io.ReadCloser, io.WriteCloser, error) {
	if opts.Verbose() {
		osenv.Logf("doCmd(machine=%q, user=%q, path=%q, daemonConnection=%d)",
			machine, user, path, daemonConnection)
	}
	var args []string
	if !opts.LocalServer() {
		cmd := opts.ShellCommand()
		if cmd == "" {
			cmd = "ssh"
			if e := os.Getenv("RSYNC_RSH"); e != "" {
				cmd = e
			}
		}

		// We use shlex.Split(), whereas rsync implements its own shell-style-like
		// parsing. The nuances likely don’t matter to any users, and if so, users
		// might prefer shell-style parsing.
		var err error
		args, err = shlex.Split(cmd)
		if err != nil {
			return nil, nil, err
		}

		if user != "" && daemonConnection == 0 /* && !dashlset */ {
			args = append(args, "-l", user)
		}

		args = append(args, machine)

		args = append(args, "rsync") // TODO: flag
	} else {
		// NOTE: tridge rsync will fork and run child_main(), we call Main() in
		// a separate goroutine below.
		args = append(args, os.Args[0])
	}

	if daemonConnection > 0 {
		args = append(args, "--server", "--daemon")
	} else {
		args = append(args, opts.ServerOptions()...)
	}
	args = append(args, ".")

	if daemonConnection == 0 {
		args = append(args, path)
	}

	if opts.Verbose() {
		osenv.Logf("args: %q", args)
	}

	if opts.LocalServer() {
		stdinrd, stdinwr := io.Pipe()
		stdoutrd, stdoutwr := io.Pipe()
		go func() {
			defer stdinrd.Close()
			defer stdoutwr.Close()
			osenv := &rsyncos.Env{
				Stdin:  stdinrd,
				Stdout: stdoutwr,
				Stderr: os.Stderr,
				// If this transfer restricts file system access, all goroutines
				// (including the other end of the connection) are affected.
				DontRestrict: true,
			}
			_, err := Main(context.Background(), osenv, args, nil)
			if err != nil {
				osenv.Logf("Main(): %v", err)
			}
		}()
		return stdoutrd, stdinwr, nil
	}

	ssh := exec.Command(args[0], args[1:]...)
	wc, err := ssh.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	rc, err := ssh.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	ssh.Stderr = osenv.Stderr
	if err := ssh.Start(); err != nil {
		return nil, nil, err
	}

	go func() {
		// TODO: correctly terminate the main process when the underlying SSH
		// process exits.
		if err := ssh.Wait(); err != nil {
			osenv.Logf("remote shell exited: %v", err)
		}
	}()

	return rc, wc, nil
}

// rsync/main.c:client_run
func ClientRun(osenv *rsyncos.Env, opts *rsyncopts.Options, conn io.ReadWriter, paths []string, negotiate bool) (*rsyncstats.TransferStats, error) {
	crd := &rsyncwire.CountingReader{R: conn}
	cwr := &rsyncwire.CountingWriter{W: conn}
	c := &rsyncwire.Conn{
		Reader: crd,
		Writer: cwr,
	}

	if negotiate {
		if err := c.WriteInt32(rsync.ProtocolVersion); err != nil {
			return nil, err
		}
		remoteProtocol, err := c.ReadInt32()
		if err != nil {
			return nil, err
		}
		if opts.Verbose() {
			osenv.Logf("remote protocol: %d", remoteProtocol)
		}
	}

	seed, err := c.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("reading seed: %v", err)
	}

	mrd := &rsyncwire.MultiplexReader{
		Env:    osenv,
		Reader: conn,
	}
	// TODO: rearchitect such that our buffer can be smaller than the largest
	// rsync message size
	rd := bufio.NewReaderSize(mrd, 256*1024)
	// Update crd to track the multiplexed reader,
	// but copy the number of bytes read.
	crd = &rsyncwire.CountingReader{
		R:         rd,
		BytesRead: crd.BytesRead,
	}
	c.Reader = crd

	if opts.Sender() {
		st := &sender.Transfer{
			Logger: osenv.Logger(),
			Opts:   opts,
			Conn:   c,
			Seed:   seed,
		}
		if opts.Verbose() {
			osenv.Logf("sender(paths=%q)", paths)
		}

		stats, err := st.Do(crd, cwr, "/", paths, nil)
		if err != nil {
			return nil, err
		}
		return stats, nil
	}

	if len(paths) != 1 {
		return nil, fmt.Errorf("BUG: expected exactly one path, got %q", paths)
	}

	rt := &receiver.Transfer{
		Logger: osenv.Logger(),
		Opts: &receiver.TransferOpts{
			Verbose: opts.Verbose(),
			DryRun:  opts.DryRun(),

			DeleteMode:        opts.DeleteMode(),
			PreserveGid:       opts.PreserveGid(),
			PreserveUid:       opts.PreserveUid(),
			PreserveLinks:     opts.PreserveLinks(),
			PreservePerms:     opts.PreservePerms(),
			PreserveDevices:   opts.PreserveDevices(),
			PreserveSpecials:  opts.PreserveSpecials(),
			PreserveTimes:     opts.PreserveMTimes(),
			PreserveHardlinks: opts.PreserveHardLinks(),
		},
		Dest: paths[0],
		Env:  osenv,
		Conn: c,
		Seed: seed,
	}
	if opts.Verbose() {
		osenv.Logf("receiving to dest=%s", rt.Dest)
	}
	if rt.Dest == "" {
		// just listing modules, not transferring anything
	} else {
		if err := os.MkdirAll(rt.Dest, 0755); err != nil {
			return nil, fmt.Errorf("MkdirAll(dest=%s): %v", rt.Dest, err)
		}
		rt.DestRoot, err = os.OpenRoot(rt.Dest)
		if err != nil {
			return nil, fmt.Errorf("OpenRoot(dest=%s): %v", rt.Dest, err)
		}
		defer rt.DestRoot.Close()
		if osenv.Restrict() {
			if err := restrict.MaybeFileSystem(nil, []string{rt.Dest}); err != nil {
				return nil, fmt.Errorf("landlock: %v", err)
			}
		}
	}

	// TODO: this is different for client/server
	// client always sends exclusion list, server always receives

	// TODO: implement support for exclusion, send exclusion list here
	const exclusionListEnd = 0
	if err := c.WriteInt32(exclusionListEnd); err != nil {
		return nil, err
	}

	if opts.Verbose() { // TODO: should be DebugGTE(RECV, 1)
		osenv.Logf("exclusion list sent")
	}

	// receive file list
	if opts.Verbose() { // TODO: should be debug (FLOG)
		osenv.Logf("receiving file list")
	}
	fileList, err := rt.ReceiveFileList()
	if err != nil {
		return nil, err
	}
	if opts.Verbose() { // TODO: should be debugGTE(FLIST, 2)
		osenv.Logf("received %d names", len(fileList))
	}

	return rt.Do(c, fileList, false)
}

func clientMain(ctx context.Context, osenv *rsyncos.Env, opts *rsyncopts.Options, remaining []string) (*rsyncstats.TransferStats, error) {
	if len(remaining) == 0 {
		// help goes to stderr when no arguments were specified
		fmt.Fprintln(osenv.Stderr, opts.Help())
		return nil, fmt.Errorf("rsync error: syntax or usage error")
	}
	if len(remaining) == 1 {
		// Usages with just one SRC arg and no DEST arg list the source files
		// instead of copying.
		dest := ""
		sources := remaining
		return rsyncMain(ctx, osenv, opts, sources, dest)
	}
	dest := remaining[len(remaining)-1]
	sources := remaining[:len(remaining)-1]
	return rsyncMain(ctx, osenv, opts, sources, dest)
}
