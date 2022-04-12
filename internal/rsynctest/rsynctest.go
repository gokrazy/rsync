package rsynctest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/gokrazy/rsync/internal/anonssh"
	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/maincmd"
	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/rsyncd"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/sys/unix"
)

type TestServer struct {
	// config
	listener  net.Listener
	listeners []rsyncdconfig.Listener

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

func New(t *testing.T, modules []rsyncd.Module, opts ...Option) *TestServer {
	ts := &TestServer{}
	for _, opt := range opts {
		opt(ts)
	}
	if len(ts.listeners) == 0 {
		ts.listeners = []rsyncdconfig.Listener{
			{Rsyncd: "localhost:0"},
		}
	}
	srv, err := rsyncd.NewServer(modules)
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

	log.Printf("listening on %s", ts.listener.Addr())
	_, port, err := net.SplitHostPort(ts.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ts.Port = port

	if ts.listeners[0].AuthorizedSSH.Address != "" {
		sshListener, err := anonssh.ListenerFromConfig(ts.listeners[0])
		if err != nil {
			t.Fatal(err)
		}
		cfg := &rsyncdconfig.Config{
			Modules: modules,
		}
		go func() {
			err := anonssh.Serve(ts.listener, sshListener, cfg, func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
				return maincmd.Main(context.Background(), args, stdin, stdout, stderr, cfg)
			})

			if errors.Is(err, net.ErrClosed) {
				return
			}

			if err != nil {
				log.Printf("%s", err.Error())
			}
		}()
	} else if ts.listeners[0].AnonSSH != "" {
		sshListener, err := anonssh.ListenerFromConfig(ts.listeners[0])
		if err != nil {
			t.Fatal(err)
		}
		cfg := &rsyncdconfig.Config{
			Modules: modules,
		}
		go func() {
			err := anonssh.Serve(ts.listener, sshListener, cfg, func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
				return maincmd.Main(context.Background(), args, stdin, stdout, stderr, cfg)
			})

			if errors.Is(err, net.ErrClosed) {
				return
			}

			if err != nil {
				log.Printf("%s", err.Error())
			}
		}()
	} else {
		go srv.Serve(context.Background(), ts.listener)
	}

	return ts
}

var rsyncVersionRe = regexp.MustCompile(`rsync\s*version ([v0-9.]+)`)

func RsyncVersion(t *testing.T) string {
	version := exec.Command("rsync", "--version")
	version.Stderr = os.Stderr
	b, err := version.Output()
	if err != nil {
		t.Fatalf("%v: %v", version.Args, err)
	}
	matches := rsyncVersionRe.FindStringSubmatch(string(b))
	if len(matches) == 0 {
		t.Fatalf("rsync: version number not found in rsync --version output")
	}
	// rsync 2.6.9 does not print a v prefix,
	// but rsync v3.2.3 does print a v prefix.
	return strings.TrimPrefix(matches[1], "v")
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
	head := bytes.Repeat(headPattern, 1*1024*1024)
	body := bytes.Repeat(bodyPattern, 1*1024*1024)
	end := bytes.Repeat(endPattern, 1*1024*1024)
	return append(append(head, body...), end...)
}

func WriteLargeDataFile(t *testing.T, source string, headPattern, bodyPattern, endPattern []byte) {
	// create large data file in source directory to be copied
	content := ConstructLargeDataFile(headPattern, bodyPattern, endPattern)
	large := filepath.Join(source, "large-data-file")
	if err := os.MkdirAll(filepath.Dir(large), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(large, content, 0644); err != nil {
		t.Fatal(err)
	}
}

func DataFileMatches(fn string, headPattern, bodyPattern, endPattern []byte) error {
	want := ConstructLargeDataFile(headPattern, bodyPattern, endPattern)
	got, err := ioutil.ReadFile(fn)
	if err != nil {
		return err
	}
	if diff := cmp.Diff(want, got); diff != "" {
		return fmt.Errorf("unexpected file contents: diff (-want +got):\n%s", diff)
	}

	return nil
}
