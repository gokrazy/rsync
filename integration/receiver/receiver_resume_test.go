package receiver_test

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/internal/testlogger"
	"github.com/gokrazy/rsync/rsyncd"
)

// TestDaemonReceiverResumesFromPartial seeds a "<name>.partial" and checks that
// a --partial push reuses it as the delta basis, then commits and drops it.
func TestDaemonReceiverResumesFromPartial(t *testing.T) {
	t.Parallel()

	rsyncBin := rsynctest.TridgeOrGTFO(t, "resume needs a real rsync client to push with --partial")

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	for _, d := range []string{source, dest} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Incompressible content so the tail can only arrive as literal data.
	const fileSize = 4 << 20
	content := make([]byte, fileSize)
	rand.New(rand.NewSource(3)).Read(content)
	if err := os.WriteFile(filepath.Join(source, "big.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	final := filepath.Join(dest, "big.bin")
	partial := final + ".partial"
	// Seed a partial with a correct ~40% prefix, as an interrupt would leave.
	if err := os.WriteFile(partial, content[:fileSize*2/5], 0o644); err != nil {
		t.Fatal(err)
	}

	port, daemonLogs := startResumeDaemon(t, dest)

	push := exec.Command(rsyncBin,
		"--archive",
		"--partial", // negotiates keep_partial=1 so the receiver resumes
		"--port="+port,
		source+"/",
		"rsync://localhost/interop/")
	push.Env = append(os.Environ(), "LANG=C.UTF-8")
	push.Stdout = testlogger.New(t)
	push.Stderr = testlogger.New(t)
	if err := push.Run(); err != nil {
		t.Fatalf("%v: %v", push.Args, err)
	}

	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, got) {
		t.Fatalf("reconstructed file != source (%d vs %d bytes)", len(got), len(content))
	}
	// A successful commit consumes the partial sidecar.
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed after commit, stat err = %v", partial, err)
	}
	// The daemon only logs a resume when it used the partial as the basis.
	waitForLog(t, daemonLogs, "resuming big.bin from partial", 5*time.Second)
}

// TestDaemonReceiverResumesAfterInterrupt severs a push mid-transfer so the
// daemon retains "<name>.partial" itself, then a second push resumes against it.
func TestDaemonReceiverResumesAfterInterrupt(t *testing.T) {
	t.Parallel()

	rsyncBin := rsynctest.TridgeOrGTFO(t, "resume-after-interrupt needs a real rsync client")

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	for _, d := range []string{source, dest} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Large and incompressible so the sever leaves a meaningful prefix.
	const fileSize = 8 << 20
	content := make([]byte, fileSize)
	rand.New(rand.NewSource(1)).Read(content)
	if err := os.WriteFile(filepath.Join(source, "big.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	port, daemonLogs := startResumeDaemon(t, dest)

	partial := filepath.Join(dest, "big.bin.partial")
	final := filepath.Join(dest, "big.bin")

	// Sever the first connection after ~1 MiB; later ones pass through.
	proxyPort := startSeveringProxy(t, net.JoinHostPort("127.0.0.1", port), 1<<20)

	// Attempt 1: the push is cut mid-stream, so it fails.
	attempt1 := exec.Command(rsyncBin,
		"--archive", "--partial",
		"--port="+proxyPort,
		source+"/", "rsync://localhost/interop/")
	attempt1.Env = append(os.Environ(), "LANG=C.UTF-8")
	attempt1.Stdout = testlogger.New(t)
	attempt1.Stderr = testlogger.New(t)
	if err := attempt1.Run(); err == nil {
		t.Fatal("attempt 1 unexpectedly succeeded; proxy should have severed it")
	}

	// The interrupt must retain <name>.partial and leave no final file.
	waitForExists(t, partial, 10*time.Second)
	if got := statSize(t, partial); got == 0 {
		t.Fatal("retained partial is empty")
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("final file must not exist after an interrupt, stat err = %v", err)
	}

	// Attempt 2: resume through the now-transparent proxy.
	attempt2 := exec.Command(rsyncBin,
		"--archive", "--partial",
		"--port="+proxyPort,
		source+"/", "rsync://localhost/interop/")
	attempt2.Env = append(os.Environ(), "LANG=C.UTF-8")
	attempt2.Stdout = testlogger.New(t)
	attempt2.Stderr = testlogger.New(t)
	if err := attempt2.Run(); err != nil {
		t.Fatalf("resume push failed: %v", err)
	}

	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, got) {
		t.Fatalf("resumed file does not match source (got %d bytes, want %d)", len(got), len(content))
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Errorf("partial should be removed after a successful commit, stat err = %v", err)
	}
	// The resume log proves attempt 2 deltaed against attempt 1's partial.
	waitForLog(t, daemonLogs, "resuming big.bin from partial", 5*time.Second)
}

// startResumeDaemon runs a writable daemon (module "interop") and returns its
// port plus an accessor for its captured log output.
func startResumeDaemon(t *testing.T, dest string) (port string, logs func() string) {
	t.Helper()
	buf := &syncBuffer{}
	srv, err := rsyncd.NewServer(
		[]rsyncd.Module{{Name: "interop", Path: dest, Writable: true}},
		rsyncd.WithStderr(nopWriteCloser{io.MultiWriter(buf, testlogger.New(t))}),
		rsyncd.DontRestrict(),
	)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx, ln) }()

	_, port, err = net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port, buf.String
}

// nopWriteCloser adapts an io.Writer to the io.WriteCloser WithStderr wants.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// syncBuffer is a concurrency-safe buffer: the daemon writes logs from its own
// goroutine while the test reads them.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startSeveringProxy relays TCP to backend but drops the first connection after
// severAfter bytes client->backend; later connections pass through untouched.
func startSeveringProxy(t *testing.T, backend string, severAfter int64) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	var conns atomic.Int32
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			sever := conns.Add(1) == 1
			go func() {
				defer client.Close()
				up, err := net.Dial("tcp", backend)
				if err != nil {
					return
				}
				defer up.Close()
				go io.Copy(client, up) // backend -> client
				if sever {
					_, _ = io.CopyN(up, client, severAfter)
					return // closes both conns, dropping the transfer
				}
				_, _ = io.Copy(up, client) // client -> backend
			}()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func waitForExists(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to exist", path)
}

func waitForLog(t *testing.T, logs func() string, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(logs(), substr) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("daemon never logged %q; logs so far:\n%s", substr, logs())
}

func statSize(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}
