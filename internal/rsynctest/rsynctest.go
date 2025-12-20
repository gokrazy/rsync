package rsynctest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/gokrazy/rsync/internal/anonssh"
	"github.com/gokrazy/rsync/internal/maincmd"
	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncostest"
	"github.com/gokrazy/rsync/internal/rsyncstats"
	"github.com/gokrazy/rsync/internal/testlogger"
	"github.com/gokrazy/rsync/rsyncclient"
	"github.com/gokrazy/rsync/rsynccmd"
	"github.com/gokrazy/rsync/rsyncd"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/sys/unix"
)

type TestServer struct {
	// config
	module       rsyncd.Module
	listener     net.Listener
	listeners    []rsyncdconfig.Listener
	dontRestrict bool

	// state
	srv *rsyncd.Server

	// Port is the port on which the test server is listening on. Useful to pass
	// to rsync’s --port option.
	Port string
}

// InteropModule is a convenience function to define an rsync module named
// “interop” with the specified path.
func InteropModule(path string) []rsyncd.Module {
	return []rsyncd.Module{
		{
			Name: "interop",
			Path: path,
		},
	}
}

// WritableInteropModule is a wrapper around InteropModule that marks the module
// as writable (not read-only).
func WritableInteropModule(path string) []rsyncd.Module {
	mods := InteropModule(path)
	mods[0].Writable = true
	return mods
}

type Option func(ts *TestServer)

func Listeners(lns []rsyncdconfig.Listener) Option {
	return func(ts *TestServer) {
		ts.listeners = lns
	}
}

func Listener(ln net.Listener) Option {
	return func(ts *TestServer) {
		ts.listener = ln
	}
}

func DontRestrict() Option {
	return func(ts *TestServer) {
		ts.dontRestrict = true
	}
}

func New(t *testing.T, modules []rsyncd.Module, opts ...Option) *TestServer {
	ctx := t.Context()

	ts := &TestServer{}
	for _, opt := range opts {
		opt(ts)
	}
	if len(ts.listeners) == 0 {
		ts.listeners = []rsyncdconfig.Listener{
			{Rsyncd: "localhost:0"},
		}
	}
	srv, err := rsyncd.NewServer(modules, rsyncd.WithStderr(testlogger.New(t)), rsyncd.DontRestrict())
	if err != nil {
		t.Fatal(err)
	}

	if ts.listener == nil {
		ln, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		ts.listener = ln
	}

	t.Logf("listening on %s", ts.listener.Addr())
	_, port, err := net.SplitHostPort(ts.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ts.Port = port

	osenv := rsyncostest.New(t)
	if ts.listeners[0].AuthorizedSSH.Address != "" {
		sshListener, err := anonssh.ListenerFromConfig(osenv, ts.listeners[0])
		if err != nil {
			t.Fatal(err)
		}
		cfg := &rsyncdconfig.Config{
			Modules: modules,
		}
		go func() {
			err := anonssh.Serve(ctx, osenv, ts.listener, sshListener, cfg, func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
				osenv := &rsyncos.Env{
					Stdin:  stdin,
					Stdout: stdout,
					Stderr: stderr,
				}
				_, err := maincmd.Main(context.Background(), osenv, args, cfg)
				return err
			})

			if errors.Is(err, net.ErrClosed) {
				return
			}

			if err != nil {
				t.Error(err)
			}
		}()
	} else if ts.listeners[0].AnonSSH != "" {
		sshListener, err := anonssh.ListenerFromConfig(osenv, ts.listeners[0])
		if err != nil {
			t.Fatal(err)
		}
		cfg := &rsyncdconfig.Config{
			Modules: modules,
		}
		go func() {
			err := anonssh.Serve(ctx, osenv, ts.listener, sshListener, cfg, func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
				osenv := &rsyncos.Env{
					Stdin:  stdin,
					Stdout: stdout,
					Stderr: stderr,
				}
				_, err := maincmd.Main(context.Background(), osenv, args, cfg)
				return err
			})

			if errors.Is(err, net.ErrClosed) {
				return
			}

			if err != nil {
				t.Error(err)
			}
		}()
	} else {
		go srv.Serve(context.Background(), ts.listener)
	}

	return ts
}

func Run(tb testing.TB, args ...string) *rsyncstats.TransferStats {
	cmd := rsynccmd.Command(args[0], args[1:]...)
	cmd.Stdout = testlogger.New(tb)
	cmd.Stderr = testlogger.New(tb)
	result, err := cmd.Run(tb.Context())
	if err != nil {
		tb.Fatal(err)
	}
	return result.Stats
}

func Output(tb testing.TB, args ...string) (stdout []byte, stderr []byte) {
	tb.Helper()
	var stdoutb, stderrb bytes.Buffer
	cmd := rsynccmd.Command(args[0], args[1:]...)
	cmd.Stdout = &stdoutb
	cmd.Stderr = &stderrb
	_, err := cmd.Run(context.Background())
	if err != nil {
		tb.Fatal(err)
	}
	return stdoutb.Bytes(), stderrb.Bytes()
}

func CombinedOutput(args ...string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := rsynccmd.Command(args[0], args[1:]...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_, err := cmd.Run(context.Background())
	return buf.Bytes(), err
}

func NewInMemory(t *testing.T, module rsyncd.Module, opts ...Option) *TestServer {
	ts := &TestServer{
		module: module,
	}
	for _, opt := range opts {
		opt(ts)
	}

	stderr := testlogger.New(t)
	rsyncdOpts := []rsyncd.Option{
		rsyncd.WithStderr(stderr),
	}
	if ts.dontRestrict {
		rsyncdOpts = append(rsyncdOpts, rsyncd.DontRestrict())
	}
	srv, err := rsyncd.NewServer([]rsyncd.Module{module}, rsyncdOpts...)
	if err != nil {
		t.Fatal(err)
	}
	ts.srv = srv
	return ts
}

type readWriter struct {
	io.Reader
	io.Writer
}

func (ts *TestServer) pipe(t *testing.T, args []string) (*sync.WaitGroup, io.ReadWriter) {
	// stdin from the view of the rsync server
	stdinrd, stdinwr := io.Pipe()
	stdoutrd, stdoutwr := io.Pipe()
	conn := rsyncd.NewConnection(stdinrd, stdoutwr, "<io.Pipe>")
	osenv := rsyncostest.New(t)
	pc := rsyncopts.NewContext(rsyncopts.NewOptionsWithGokrazyDefaults(osenv))
	if err := pc.ParseArguments(osenv, args); err != nil {
		t.Fatalf("parsing server args: %v", err)
	}
	t.Logf("pc.RemainingArgs=%q", pc.RemainingArgs)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := ts.srv.InternalHandleConn(t.Context(), conn, &ts.module, pc)
		if err != nil {
			t.Error(err)
		}
	}()

	rw := &readWriter{
		Reader: stdoutrd,
		Writer: stdinwr,
	}
	return &wg, rw
}

func (ts *TestServer) RunClient(t *testing.T, args []string, remaining []string) *rsyncstats.TransferStats {
	stderr := testlogger.New(t)
	cl, err := rsyncclient.New(args, rsyncclient.WithStderr(stderr), rsyncclient.DontRestrict())
	if err != nil {
		t.Fatal(err)
	}
	wg, rw := ts.pipe(t, cl.ServerCommandOptions("./"))
	res, err := cl.Run(t.Context(), rw, remaining)
	if err != nil {
		t.Fatal(err)
	}
	// Ensure an error would be displayed, if any.
	wg.Wait()
	return res.Stats
}

func CommandMain(m *testing.M) error {
	osenv := &rsyncos.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	if len(os.Args) > 1 && os.Args[1] == "localhost" {
		// Strip first 2 args (./rsync.test localhost) from command line:
		// rsync(1) is calling this process as a remote shell.
		os.Args = os.Args[2:]
		if _, err := maincmd.Main(context.Background(), osenv, os.Args, nil); err != nil {
			return err
		}
	} else if len(os.Args) > 1 && os.Args[1] == "--server" {
		// gokr-rsync is calling this process as a local daemon.
		if _, err := maincmd.Main(context.Background(), osenv, os.Args, nil); err != nil {
			return err
		}
	} else {
		os.Exit(m.Run())
	}
	return nil
}

func CreateDummyDeviceFiles(t *testing.T, dir string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	char := filepath.Join(dir, "char")
	// major 1, minor 5, like /dev/zero
	if err := unix.Mknod(char, 0600|syscall.S_IFCHR, int(unix.Mkdev(1, 5))); err != nil {
		t.Fatal(err)
	}

	block := filepath.Join(dir, "block")
	// major 242, minor 9, like /dev/nvme0
	if err := unix.Mknod(block, 0600|syscall.S_IFBLK, int(unix.Mkdev(242, 9))); err != nil {
		t.Fatal(err)
	}

	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
}

func VerifyDummyDeviceFiles(t *testing.T, source, dest string) {
	{
		sourcest, err := os.Stat(filepath.Join(source, "char"))
		if err != nil {
			t.Fatal(err)
		}
		destst, err := os.Stat(filepath.Join(dest, "char"))
		if err != nil {
			t.Fatal(err)
		}
		if destst.Mode().Type()&os.ModeCharDevice == 0 {
			t.Fatalf("unexpected type: got %v, want character device", destst.Mode())
		}
		destsys, ok := destst.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("stat does not contain rdev")
		}
		sourcesys, ok := sourcest.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("stat does not contain rdev")
		}
		if got, want := destsys.Rdev, sourcesys.Rdev; got != want {
			t.Fatalf("unexpected rdev: got %v, want %v", got, want)
		}
	}

	{
		sourcest, err := os.Stat(filepath.Join(source, "block"))
		if err != nil {
			t.Fatal(err)
		}
		destst, err := os.Stat(filepath.Join(dest, "block"))
		if err != nil {
			t.Fatal(err)
		}
		if destst.Mode().Type()&os.ModeDevice == 0 ||
			destst.Mode().Type()&os.ModeCharDevice != 0 {
			t.Fatalf("unexpected type: got %v, want block device", destst.Mode())
		}
		destsys, ok := destst.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("stat does not contain rdev")
		}
		sourcesys, ok := sourcest.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("stat does not contain rdev")
		}
		if got, want := destsys.Rdev, sourcesys.Rdev; got != want {
			t.Fatalf("unexpected rdev: got %v, want %v", got, want)
		}
	}

	{
		st, err := os.Stat(filepath.Join(dest, "fifo"))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Type()&os.ModeNamedPipe == 0 {
			t.Fatalf("unexpected type: got %v, want fifo", st.Mode())
		}
	}

	{
		st, err := os.Stat(filepath.Join(dest, "sock"))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Type()&os.ModeSocket == 0 {
			t.Fatalf("unexpected type: got %v, want socket", st.Mode())
		}
	}
}

func ConstructLargeDataFile(headPattern, bodyPattern, endPattern []byte) []byte {
	// create large data file in source directory to be copied
	head := bytes.Repeat(headPattern, 1*1024)
	body := bytes.Repeat(bodyPattern, 1*1024)
	end := bytes.Repeat(endPattern, 1*1024)
	return append(append(head, body...), end...)
}

func WriteLargeDataFile(t *testing.T, source string, headPattern, bodyPattern, endPattern []byte) {
	// create large data file in source directory to be copied
	content := ConstructLargeDataFile(headPattern, bodyPattern, endPattern)
	large := filepath.Join(source, "large-data-file")
	if err := os.MkdirAll(filepath.Dir(large), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, content, 0644); err != nil {
		t.Fatal(err)
	}
}

func DataFileMatches(fn string, headPattern, bodyPattern, endPattern []byte) error {
	want := ConstructLargeDataFile(headPattern, bodyPattern, endPattern)
	got, err := os.ReadFile(fn)
	if err != nil {
		return err
	}
	// fast path: using bytes.Equal for an equality check uses much less
	// resources compared to cmp.Diff.
	if bytes.Equal(want, got) {
		return nil
	}
	if diff := cmp.Diff(want, got); diff != "" {
		return fmt.Errorf("unexpected file contents: diff (-want +got):\n%s", diff)
	}

	return nil
}

type discovered struct {
	rsync       string // path to any rsync, typically "rsync"
	tridgeRsync string // path to tridge rsync if discovered
}

func (d discovered) anyRsync() string {
	if d.tridgeRsync != "" {
		return d.tridgeRsync
	}
	return d.rsync
}

var discoverOnce = sync.OnceValue(func() discovered {
	// For tests that need tridge rsync, explicitly check
	// a few well-known locations.
	locations := []string{
		// If rsync is installed from homebrew,
		// that will typically be the latest / preferred version.
		"/opt/homebrew/bin/rsync",
	}
	if runtime.GOOS == "darwin" {
		// macOS 15 replaced rsync with a wrapper that
		// dispatches to openrsync by default, unless
		// it finds an option that it knows needs tridge rsync.
		// They will probably stop shipping tridge rsync
		// eventually, but for now check this location:
		locations = append(locations, "/usr/libexec/rsync/rsync.samba")
	}
	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return discovered{tridgeRsync: loc}
		}
	}
	version, err := exec.Command("rsync", "--version").Output()
	if err != nil {
		return discovered{}
	}
	if strings.Contains(string(version), "openrsync:") {
		return discovered{rsync: "rsync"}
	}
	return discovered{tridgeRsync: "rsync"}
})

func TridgeOrGTFO(t *testing.T, reason string) string {
	discovered := discoverOnce()
	if discovered.anyRsync() == "" {
		// Gotta set some boundaries.
		// We need *some* rsync.
		t.Fatalf("no rsync installed")
	}
	if discovered.tridgeRsync == "" {
		// we did not find tridge rsync and the default rsync is openrsync
		t.Skipf("tridge rsync not found, cannot run this test: %v", reason)
	}
	return discovered.tridgeRsync
}

func AnyRsync(t *testing.T) string {
	any := discoverOnce().anyRsync()
	if any == "" {
		// Gotta set some boundaries.
		// We need *some* rsync.
		t.Fatalf("no rsync installed")
	}
	return any
}

var rsyncVersionRe = regexp.MustCompile(`rsync\s*version ([v0-9.]+)`)
var rsyncVersionOnce = sync.OnceValue(func() string {
	any := discoverOnce().anyRsync()
	version := exec.Command(any, "--version")
	version.Stderr = os.Stderr
	b, err := version.Output()
	if err != nil {
		return fmt.Sprintf("BUG: %s --version: %v", any, err)
	}
	matches := rsyncVersionRe.FindStringSubmatch(string(b))
	if len(matches) == 0 {
		return fmt.Sprintf("BUG: rsync version number not found in output %q", string(b))
	}
	// rsync 2.6.9 does not print a v prefix,
	// but rsync v3.2.3 does print a v prefix.
	return strings.TrimPrefix(matches[1], "v")
})

func RsyncVersion(t *testing.T) string {
	any := discoverOnce().anyRsync()
	if any == "" {
		// Gotta set some boundaries.
		// We need *some* rsync.
		t.Fatalf("no rsync installed")
	}
	return rsyncVersionOnce()
}
