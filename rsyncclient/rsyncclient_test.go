package rsyncclient_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncostest"
	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/internal/testlogger"
	"github.com/gokrazy/rsync/rsyncclient"
	"github.com/gokrazy/rsync/rsyncd"
	"github.com/google/go-cmp/cmp"
)

func ExampleClient_Run_receiveFromSubprocess() {
	args, src, dest := []string{"-av"}, "/usr/share/man", "/tmp/man"
	client, err := rsyncclient.New(args)
	if err != nil {
		log.Fatal(err)
	}

	// Start an rsynccmd server and run an rsynccmd client on its stdin/stdout.
	rsynccmd := exec.Command("rsync", client.ServerCommandOptions(src)...)
	stdin, err := rsynccmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	stdout, err := rsynccmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := rsynccmd.Start(); err != nil {
		log.Fatal(err)
	}
	// Create an io.ReadWriteCloser from a ReadCloser and a WriteCloser.
	rw := &rsync.BothCloser{
		ReadCloser:  stdout, // The client reads from the server's stdout.
		WriteCloser: stdin,  // The client writes to the server's stdin.
	}
	if _, err := client.Run(context.Background(), rw, []string{dest}); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Run_sendToGoroutine() {
	ctx := context.Background()

	args, src, dest := []string{"-av"}, "/usr/share/man", "/tmp/man"
	client, err := rsyncclient.New(args, rsyncclient.WithSender())
	if err != nil {
		log.Fatal(err)
	}

	// Start an srv server and run an srv client on
	// an io.Pipe()-backend stdin/stdout.
	srv, err := rsyncd.NewServer(nil)
	if err != nil {
		log.Fatal(err)
	}
	// stdin from the view of the rsync server
	stdinrd, stdinwr := io.Pipe()
	stdoutrd, stdoutwr := io.Pipe()
	go func() {
		conn := rsyncd.NewConnection(stdinrd, stdoutwr, "<io.Pipe>")
		osenv := &rsyncos.Env{Stderr: os.Stderr}
		pc := rsyncopts.NewContext(rsyncopts.NewOptions(osenv))
		if err := pc.ParseArguments(osenv, client.ServerCommandOptions(dest)); err != nil {
			log.Fatalf("parsing server args: %v", err)
		}
		if err := srv.InternalHandleConn(ctx, conn, nil, pc); err != nil {
			log.Fatal(err)
		}
	}()

	// Create an io.ReadWriteCloser from a ReadCloser and a WriteCloser.
	rw := &rsync.BothCloser{
		ReadCloser:  stdoutrd, // The client reads from the server's stdout.
		WriteCloser: stdinwr,  // The client writes to the server's stdin.
	}

	if _, err := client.Run(ctx, rw, []string{src}); err != nil {
		log.Fatal(err)
	}
}

func TestClientCommand(t *testing.T) {
	t.Parallel()

	client, err := rsyncclient.New([]string{"-av"})
	if err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	dest := filepath.Join(tmp, "dest")
	// Start an rsynccmd process directly for the server part of the test.
	rsynccmd := exec.Command(rsynctest.AnyRsync(t), client.ServerCommandOptions(".")...)
	rsynccmd.Dir = tmp
	wc, err := rsynccmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	rc, err := rsynccmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// Create an io.ReadWriteCloser from a ReadCloser and a WriteCloser.
	rw := &rsync.BothCloser{
		ReadCloser:  rc,
		WriteCloser: wc,
	}
	if err := rsynccmd.Start(); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Run(t.Context(), rw, []string{dest}); err != nil {
		t.Fatal(err)
	}
}

func TestClientServerModule(t *testing.T) {
	t.Parallel()

	stderr := testlogger.New(t)
	tmp := t.TempDir()

	src := filepath.Join(tmp, "src")
	dest := filepath.Join(tmp, "dest")
	const hello = "world"
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hello"), []byte(hello), 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{"-av"}
	client, err := rsyncclient.New(args, rsyncclient.WithStderr(stderr))
	if err != nil {
		t.Fatal(err)
	}

	mod := rsyncd.Module{
		Name: "tmp",
		Path: src,
	}
	srv, err := rsyncd.NewServer([]rsyncd.Module{mod}, rsyncd.WithStderr(stderr))
	if err != nil {
		t.Fatal(err)
	}
	// stdin from the view of the rsync server
	stdinrd, stdinwr := io.Pipe()
	stdoutrd, stdoutwr := io.Pipe()
	conn := rsyncd.NewConnection(stdinrd, stdoutwr, "<io.Pipe>")
	osenv := rsyncostest.New(t)
	pc := rsyncopts.NewContext(rsyncopts.NewOptions(osenv))
	if err := pc.ParseArguments(osenv, client.ServerCommandOptions("./")); err != nil {
		t.Fatalf("parsing server args: %v", err)
	}
	t.Logf("pc.RemainingArgs=%q", pc.RemainingArgs)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := srv.InternalHandleConn(t.Context(), conn, &mod, pc)
		if err != nil {
			t.Error(err)
		}
	}()

	// Create an io.ReadWriteCloser from a ReadCloser and a WriteCloser.
	rw := &rsync.BothCloser{
		ReadCloser:  stdoutrd,
		WriteCloser: stdinwr,
	}
	if _, err := client.Run(t.Context(), rw, []string{dest}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(hello)) {
		t.Errorf("hello: unexpected contents: diff (-want +got):\n%s", cmp.Diff([]byte(hello), got))
	}

	// Ensure an error would be displayed, if any.
	wg.Wait()
}

// like TestClientServerModule, but without a module,
// i.e. using the command calling convention.
func TestClientServerCommand(t *testing.T) {
	t.Parallel()

	stderr := testlogger.New(t)
	tmp := t.TempDir()

	src := filepath.Join(tmp, "src") + "/"
	dest := filepath.Join(tmp, "dest")
	const hello = "world"
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hello"), []byte(hello), 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{"-av"}
	client, err := rsyncclient.New(args)
	if err != nil {
		t.Fatal(err)
	}

	srv, err := rsyncd.NewServer(nil, rsyncd.WithStderr(stderr))
	if err != nil {
		t.Fatal(err)
	}
	// stdin from the view of the rsync server
	stdinrd, stdinwr := io.Pipe()
	stdoutrd, stdoutwr := io.Pipe()
	conn := rsyncd.NewConnection(stdinrd, stdoutwr, "<io.Pipe>")
	osenv := rsyncostest.New(t)
	pc := rsyncopts.NewContext(rsyncopts.NewOptions(osenv))
	if err := pc.ParseArguments(osenv, client.ServerCommandOptions(src)); err != nil {
		t.Fatalf("parsing server args: %v", err)
	}
	t.Logf("pc.RemainingArgs=%q", pc.RemainingArgs)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := srv.InternalHandleConn(t.Context(), conn, nil, pc)
		if err != nil {
			t.Error(err)
		}
	}()

	// Create an io.ReadWriteCloser from a ReadCloser and a WriteCloser.
	rw := &rsync.BothCloser{
		ReadCloser:  stdoutrd,
		WriteCloser: stdinwr,
	}
	if _, err := client.Run(t.Context(), rw, []string{dest}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(hello)) {
		t.Errorf("hello: unexpected contents: diff (-want +got):\n%s", cmp.Diff([]byte(hello), got))
	}

	// Ensure an error would be displayed, if any.
	wg.Wait()
}

// like TestClientServerCommand, but sending data instead of receiving.
func TestClientServerCommandSender(t *testing.T) {
	t.Parallel()

	stderr := testlogger.New(t)
	tmp := t.TempDir()

	src := filepath.Join(tmp, "src") + "/"
	dest := filepath.Join(tmp, "dest")
	const hello = "world"
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hello"), []byte(hello), 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{"-av"}
	client, err := rsyncclient.New(args, rsyncclient.WithSender())
	if err != nil {
		t.Fatal(err)
	}

	srv, err := rsyncd.NewServer(nil, rsyncd.WithStderr(stderr))
	if err != nil {
		t.Fatal(err)
	}
	// stdin from the view of the rsync server
	stdinrd, stdinwr := io.Pipe()
	stdoutrd, stdoutwr := io.Pipe()
	conn := rsyncd.NewConnection(stdinrd, stdoutwr, "<io.Pipe>")
	osenv := rsyncostest.New(t)
	pc := rsyncopts.NewContext(rsyncopts.NewOptions(osenv))
	if err := pc.ParseArguments(osenv, client.ServerCommandOptions(dest)); err != nil {
		t.Fatalf("parsing server args: %v", err)
	}
	t.Logf("pc.RemainingArgs=%q", pc.RemainingArgs)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := srv.InternalHandleConn(t.Context(), conn, nil, pc)
		if err != nil {
			t.Error(err)
		}
	}()

	// Create an io.ReadWriteCloser from a ReadCloser and a WriteCloser.
	rw := &rsync.BothCloser{
		ReadCloser:  stdoutrd,
		WriteCloser: stdinwr,
	}
	if _, err := client.Run(t.Context(), rw, []string{src}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(hello)) {
		t.Errorf("hello: unexpected contents: diff (-want +got):\n%s", cmp.Diff([]byte(hello), got))
	}

	// Ensure an error would be displayed, if any.
	wg.Wait()
}
