//go:build linux

package receiver_test

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/rsyncd"
)

func TestReceiverSparse(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	const hole = 8 * 1024 * 1024
	want := bytes.Repeat([]byte{0x11}, 5000)                 // initial data
	want = append(want, make([]byte, hole)...)               // a hole
	want = append(want, bytes.Repeat([]byte{0xee}, 5000)...) // more data
	want = append(want, make([]byte, hole)...)               // ending in a hole
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "disk.img"), want, 0644); err != nil {
		t.Fatal(err)
	}

	srv := rsynctest.NewInMemory(t, rsyncd.Module{
		Name: "interop",
		Path: source,
	})

	fn := filepath.Join(dest, "disk.img")
	srv.RunClient(t, []string{"-aS"}, "./", []string{dest})
	got, err := os.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected file contents (got %d bytes, want %d bytes)", len(got), len(want))
	}
	st, err := os.Stat(fn)
	if err != nil {
		t.Fatal(err)
	}
	const blockSize = 512 // DEV_BSIZE, see https://unix.stackexchange.com/a/669545
	if got := st.Sys().(*syscall.Stat_t).Blocks * blockSize; got >= hole {
		t.Errorf("destination file is not sparse: %d bytes allocated", got)
	}

	// Modify the file (changing its size, so that it is not skipped) and sync
	// again, so that holes are now created from blocks which match the
	// existing destination file.
	want = append(want, 0x22)
	if err := os.WriteFile(filepath.Join(source, "disk.img"), want, 0644); err != nil {
		t.Fatal(err)
	}
	srv.RunClient(t, []string{"-aS"}, "./", []string{dest})
	got, err = os.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected file contents (got %d bytes, want %d bytes)", len(got), len(want))
	}
	st, err = os.Stat(fn)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Sys().(*syscall.Stat_t).Blocks * blockSize; got >= hole {
		t.Errorf("destination file is not sparse: %d bytes allocated", got)
	}
}
