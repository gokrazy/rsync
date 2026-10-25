package rsyncd_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/rsyncd"
)

func TestContextCancellation(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	// rsynctest.New serves on context.Background(),
	// so we manually construct an rsyncd.Server.
	srv, err := rsyncd.NewServer(rsynctest.InteropModule(tmp), rsyncd.DontRestrict())
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(t.Context())
	go srv.Serve(ctx, ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Read the server greeting to make sure the server goroutine
	// is stuck in a Read() call when we cancel.
	rd := bufio.NewReader(conn)
	if _, err := rd.ReadSlice('\n'); err != nil {
		t.Fatal(err)
	}
	cancel()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(conn); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatalf("daemon unexpectedly did not close the connection")
		}
		t.Fatal(err)
	}
}
