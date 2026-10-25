package receiver_test

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

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/progress"
	"github.com/gokrazy/rsync/internal/receiver"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

const (
	verboseBit = 1 << iota
	dryRunBit
	serverBit
	progressBit
	deleteModeBit
	preserveGidBit
	preserveUidBit
	preserveLinksBit
)

const (
	preservePermsBit = 1 << iota
	preserveDevicesBit
	preserveSpecialsBit
	preserveTimesBit
	preserveHardlinksBit
	ignoreTimesBit
	alwaysChecksumBit
	doFsyncBit
)

const (
	keepPartialBit = 1 << iota
	sparseFilesBit
)

// fuzzTransfer creates a receiver.Transfer such that:
//
// 1. The fuzz test runs at maximum throughput (no logging).
// 2. Options are reachable for the fuzzer (set based on fuzz data).
func fuzzTransfer(t *testing.T, tmp string, root *os.Root, opts1, opts2, opts3 byte, wire []byte) *receiver.Transfer {
	return &receiver.Transfer{
		// config
		Logger: log.New(io.Discard),
		Opts: &receiver.TransferOpts{
			// Vary options based on fuzzer data.
			Verbose:       opts1&verboseBit != 0,
			DryRun:        opts1&dryRunBit != 0,
			Server:        opts1&serverBit != 0,
			Progress:      opts1&progressBit != 0,
			DeleteMode:    opts1&deleteModeBit != 0,
			PreserveGid:   opts1&preserveGidBit != 0,
			PreserveUid:   opts1&preserveUidBit != 0,
			PreserveLinks: opts1&preserveLinksBit != 0,

			PreservePerms:     opts2&preservePermsBit != 0,
			PreserveDevices:   opts2&preserveDevicesBit != 0,
			PreserveSpecials:  opts2&preserveSpecialsBit != 0,
			PreserveTimes:     opts2&preserveTimesBit != 0,
			PreserveHardlinks: opts2&preserveHardlinksBit != 0,
			IgnoreTimes:       opts2&ignoreTimesBit != 0,
			AlwaysChecksum:    opts2&alwaysChecksumBit != 0,
			DoFsync:           opts2&doFsyncBit != 0,

			KeepPartial: opts3&keepPartialBit != 0,
			SparseFiles: opts3&sparseFilesBit != 0,

			InfoGTE: func(rsyncopts.InfoLevel, uint16) bool {
				return false // TODO: Vary based on fuzzer data?
			},
			DebugGTE: func(rsyncopts.DebugLevel, uint16) bool {
				return false // TODO: Vary based on fuzzer data?
			},
		},
		Dest:     tmp,
		DestRoot: root,
		Env: &rsyncos.Env{
			Stdout: rsynctest.NopClose(io.Discard),
			Stderr: rsynctest.NopClose(io.Discard),
		},
		Progress: progress.NewPrinter(io.Discard, time.Now),
		Conn: &rsyncwire.Conn{
			Reader: io.NopCloser(bytes.NewReader(wire)),
			Writer: rsynctest.NopClose(io.Discard),
		},
		// Seed is not worth varying, it is used for MD4,
		// but never influences control flow.
		Seed: 1,
		// No need to vary state/output fields like
		// IOErrors, Users and Groups.
	}
}

func allocated() uint64 {
	s := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

func FuzzReceiveFileList(f *testing.F) {
	// This fuzz test does not write to root,
	// so create an empty temporary directory (once),
	// which is never written to.
	tmp := f.TempDir()
	root, err := os.OpenRoot(tmp)
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { root.Close() })

	const allocLimit = 16 // MB

	// TODO: provide seed bytes?
	f.Fuzz(func(t *testing.T, opts1, opts2, opts3 byte, wire []byte) {
		rt := fuzzTransfer(t, tmp, root, opts1, opts2, opts3, wire)
		before := allocated()
		// If ReceiveFileList panics, that is a fuzzer finding.
		rt.ReceiveFileList()
		after := allocated()
		if n := after - before; n > allocLimit*1024*1024 {
			t.Fatalf("allocated %d MB for a %d byte input", n/1024/1024, len(wire))
		}
	})
}

func FuzzRecvFiles(f *testing.F) {
	// This fuzz test does not write to root,
	// so create an empty temporary directory (once),
	// which is never written to.
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
	var files []*receiver.File
	for _, name := range slices.Sorted(maps.Keys(testFiles)) {
		content := testFiles[name]
		fn := filepath.Join(tmp, name)
		if err := os.WriteFile(fn, content, 0644); err != nil {
			f.Fatal(err)
		}
		files = append(files, &receiver.File{
			Name:   name,
			Length: int64(len(content)),
			// ModTime    time.Time
			Mode: rsync.S_IFREG | 0o644,
			// Uid        int32
			// Gid        int32
			// LinkTarget string
			// Rdev       int32
			// Checksum   [rsyncchecksum.Size]byte
		})
	}

	const allocLimit = 16 // MB

	// TODO: provide seed bytes?
	f.Fuzz(func(t *testing.T, opts1, opts2, opts3 byte, wire []byte) {
		rt := fuzzTransfer(t, tmp, root, opts1, opts2, opts3, wire)
		before := allocated()
		// If ReceiveFileList panics, that is a fuzzer finding.
		rt.RecvFiles(files)
		after := allocated()
		if n := after - before; n > allocLimit*1024*1024 {
			t.Fatalf("allocated %d MB for a %d byte input", n/1024/1024, len(wire))
		}
	})
}
