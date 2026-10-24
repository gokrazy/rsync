package sender

import (
	"bytes"
	"io"
	"runtime/metrics"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/progress"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

type nopCloser struct{}

func (*nopCloser) Close() error { return nil }

func nopClose(w io.Writer) io.WriteCloser {
	return struct {
		io.Writer
		io.Closer
	}{
		Writer: w,
		Closer: &nopCloser{},
	}
}

// fuzzTransfer creates a Transfer such that:
//
// 1. The fuzz test runs at maximum throughput (no logging).
func fuzzTransfer(t *testing.T, wire []byte) *Transfer {
	osenv := &rsyncos.Env{
		Stdout: nopClose(io.Discard),
		Stderr: nopClose(io.Discard),
	}
	pc := rsyncopts.NewContext(rsyncopts.NewOptionsWithGokrazyDefaults(osenv))
	err := pc.ParseArguments(osenv, []string{"--server", "--sender", ".", "."})
	if err != nil {
		t.Fatal(err)
	}
	return &Transfer{
		// config
		Logger:   log.New(io.Discard),
		Opts:     pc.Options,
		Env:      osenv,
		Progress: progress.NewPrinter(io.Discard, time.Now),
		Conn: &rsyncwire.Conn{
			Reader: io.NopCloser(bytes.NewReader(wire)),
			Writer: nopClose(io.Discard),
		},
		// Seed is not worth varying, it is used for MD4,
		// but never influences control flow.
		Seed: 1,
	}
}

func allocated() uint64 {
	s := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

func FuzzRecvFilterList(f *testing.F) {
	const allocLimit = 16 // MB

	f.Fuzz(func(t *testing.T, wire []byte) {
		st := fuzzTransfer(t, wire)
		before := allocated()
		// If RecvFilterList or l.matches panics, that is a fuzzer finding.
		l, err := RecvFilterList(st.Conn)
		after := allocated()
		if n := after - before; n > allocLimit*1024*1024 {
			t.Fatalf("allocated %d MB for a %d byte input", n/1024/1024, len(wire))
		}
		if err != nil {
			return
		}
		for _, name := range []string{".", "foo", "dir/foo.c", ""} {
			l.matches(name)
		}
	})
}

func FuzzReceiveSums(f *testing.F) {
	const allocLimit = 16 // MB

	f.Fuzz(func(t *testing.T, wire []byte) {
		st := fuzzTransfer(t, wire)
		before := allocated()
		// If receiveSums panics, that is a fuzzer finding.
		head, err := st.receiveSums()
		after := allocated()
		if n := after - before; n > allocLimit*1024*1024 {
			t.Fatalf("allocated %d MB for a %d byte input", n/1024/1024, len(wire))
		}
		if err != nil {
			return
		}
		_ = head
	})
}
