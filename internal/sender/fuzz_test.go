package sender

import (
	"bytes"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime/metrics"
	"slices"
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

func FuzzSendFiles(f *testing.F) {
	tmp := f.TempDir()
	root, err := os.OpenRoot(tmp)
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { root.Close() })

	// Create a couple of test files for SendFiles.
	medium := make([]byte, 4096)
	for i := range medium {
		medium[i] = byte(i * 7)
	}
	testFiles := map[string][]byte{
		"empty":  nil,
		"small":  []byte("hello"),
		"medium": medium,
	}
	var files []file
	for _, name := range slices.Sorted(maps.Keys(testFiles)) {
		content := testFiles[name]
		fn := filepath.Join(tmp, name)
		if err := os.WriteFile(fn, content, 0644); err != nil {
			f.Fatal(err)
		}
		files = append(files, file{
			source:  newOSRootSource(root),
			path:    name,
			Wpath:   name,
			regular: true,
			Length:  int64(len(content)),
		})
	}

	const allocLimit = 16 // MB

	f.Fuzz(func(t *testing.T, wire []byte) {
		st := fuzzTransfer(t, wire)
		before := allocated()
		// If SendFiles panics, that is a fuzzer finding.
		err := st.SendFiles(&fileList{
			Files: files,
		})
		after := allocated()
		if n := after - before; n > allocLimit*1024*1024 {
			t.Fatalf("allocated %d MB for a %d byte input", n/1024/1024, len(wire))
		}
		if err != nil {
			return
		}
	})
}
