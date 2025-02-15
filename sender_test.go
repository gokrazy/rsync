package rsync_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/receivermaincmd"
	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/google/go-cmp/cmp"
	"github.com/google/renameio/v2"
)

func TestSender(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(source, "hello")
	if err := os.WriteFile(hello, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	mtime, err := time.Parse(time.RFC3339, "2009-11-10T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(hello, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("hello", filepath.Join(source, "hey")); err != nil {
		t.Fatal(err)
	}

	no := filepath.Join(source, "no")
	if err := os.WriteFile(no, []byte("no"), 0666); err != nil {
		t.Fatal(err)
	}
	uid, gid, verifyUid := setUid(t, no)

	devices := filepath.Join(source, "devices")
	if os.Getuid() == 0 {
		rsynctest.CreateDummyDeviceFiles(t, devices)
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	args := []string{
		"gokr-rsync",
		"-aH",
		source + "/",
		"rsync://localhost:" + srv.Port + "/interop/",
	}
	firstStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
	{
		got, err := os.Readlink(filepath.Join(dest, "hey"))
		if err != nil {
			t.Fatal(err)
		}
		want := "hello"
		if got != want {
			t.Fatalf("unexpected link target: got %q, want %q", got, want)
		}
	}
	if verifyUid {
		st, err := os.Stat(filepath.Join(dest, "no"))
		if err != nil {
			t.Fatal(err)
		}
		stt := st.Sys().(*syscall.Stat_t)
		if got, want := int(stt.Uid), uid; got != want {
			t.Errorf("unexpected uid: got %d, want %d", got, want)
		}
		if got, want := int(stt.Gid), gid; got != want {
			t.Errorf("unexpected gid: got %d, want %d", got, want)
		}
	}
	if os.Getuid() == 0 {
		rsynctest.VerifyDummyDeviceFiles(t, devices, filepath.Join(dest, "devices"))
	}

	incrementalStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	if incrementalStats.Written >= firstStats.Written {
		t.Fatalf("incremental run unexpectedly not more efficient than first run: incremental wrote %d bytes, first wrote %d bytes", incrementalStats.Written, firstStats.Written)
	}

	// Make a change that is invisible with our current settings:
	// change the file contents without changing size and mtime.
	if err := os.WriteFile(hello, []byte("moon!"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(hello, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	// Replace the dest symlink to see if it will be restored
	if err := renameio.Symlink("wrong", filepath.Join(dest, "hey")); err != nil {
		t.Fatal(err)
	}

	if _, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout); err != nil {
		t.Fatal(err)
	}

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
	{
		got, err := os.Readlink(filepath.Join(dest, "hey"))
		if err != nil {
			t.Fatal(err)
		}
		want := "hello"
		if got != want {
			t.Fatalf("unexpected link target: got %q, want %q", got, want)
		}
	}
}

// like TestSender, but without a trailing slash, i.e. do not copy directory
// contents, but the directory itself.
func TestSenderNoSlash(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(source, "hello")
	if err := os.WriteFile(hello, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	mtime, err := time.Parse(time.RFC3339, "2009-11-10T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(hello, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("hello", filepath.Join(source, "hey")); err != nil {
		t.Fatal(err)
	}

	no := filepath.Join(source, "no")
	if err := os.WriteFile(no, []byte("no"), 0666); err != nil {
		t.Fatal(err)
	}
	uid, gid, verifyUid := setUid(t, no)

	devices := filepath.Join(source, "devices")
	if os.Getuid() == 0 {
		rsynctest.CreateDummyDeviceFiles(t, devices)
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	args := []string{
		"gokr-rsync",
		"-aH",
		source,
		"rsync://localhost:" + srv.Port + "/interop/",
	}
	firstStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}

	dest = filepath.Join(dest, "source")

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
	{
		got, err := os.Readlink(filepath.Join(dest, "hey"))
		if err != nil {
			t.Fatal(err)
		}
		want := "hello"
		if got != want {
			t.Fatalf("unexpected link target: got %q, want %q", got, want)
		}
	}
	if verifyUid {
		st, err := os.Stat(filepath.Join(dest, "no"))
		if err != nil {
			t.Fatal(err)
		}
		stt := st.Sys().(*syscall.Stat_t)
		if got, want := int(stt.Uid), uid; got != want {
			t.Errorf("unexpected uid: got %d, want %d", got, want)
		}
		if got, want := int(stt.Gid), gid; got != want {
			t.Errorf("unexpected gid: got %d, want %d", got, want)
		}
	}
	if os.Getuid() == 0 {
		rsynctest.VerifyDummyDeviceFiles(t, devices, filepath.Join(dest, "devices"))
	}

	incrementalStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	if incrementalStats.Written >= firstStats.Written {
		t.Fatalf("incremental run unexpectedly not more efficient than first run: incremental wrote %d bytes, first wrote %d bytes", incrementalStats.Written, firstStats.Written)
	}

	// Make a change that is invisible with our current settings:
	// change the file contents without changing size and mtime.
	if err := os.WriteFile(hello, []byte("moon!"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(hello, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	// Replace the dest symlink to see if it will be restored
	if err := renameio.Symlink("wrong", filepath.Join(dest, "hey")); err != nil {
		t.Fatal(err)
	}

	if _, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout); err != nil {
		t.Fatal(err)
	}

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
	{
		got, err := os.Readlink(filepath.Join(dest, "hey"))
		if err != nil {
			t.Fatal(err)
		}
		want := "hello"
		if got != want {
			t.Fatalf("unexpected link target: got %q, want %q", got, want)
		}
	}
}

// like TestSender, but both source and dest are local directories
func TestSenderBothLocal(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(source, "hello")
	if err := os.WriteFile(hello, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	mtime, err := time.Parse(time.RFC3339, "2009-11-10T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(hello, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("hello", filepath.Join(source, "hey")); err != nil {
		t.Fatal(err)
	}

	no := filepath.Join(source, "no")
	if err := os.WriteFile(no, []byte("no"), 0666); err != nil {
		t.Fatal(err)
	}
	uid, gid, verifyUid := setUid(t, no)

	devices := filepath.Join(source, "devices")
	if os.Getuid() == 0 {
		rsynctest.CreateDummyDeviceFiles(t, devices)
	}

	args := []string{
		"gokr-rsync",
		"-aH",
		source,
		dest,
	}
	firstStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}

	dest = filepath.Join(dest, "source")

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
	{
		got, err := os.Readlink(filepath.Join(dest, "hey"))
		if err != nil {
			t.Fatal(err)
		}
		want := "hello"
		if got != want {
			t.Fatalf("unexpected link target: got %q, want %q", got, want)
		}
	}
	if verifyUid {
		st, err := os.Stat(filepath.Join(dest, "no"))
		if err != nil {
			t.Fatal(err)
		}
		stt := st.Sys().(*syscall.Stat_t)
		if got, want := int(stt.Uid), uid; got != want {
			t.Errorf("unexpected uid: got %d, want %d", got, want)
		}
		if got, want := int(stt.Gid), gid; got != want {
			t.Errorf("unexpected gid: got %d, want %d", got, want)
		}
	}
	if os.Getuid() == 0 {
		rsynctest.VerifyDummyDeviceFiles(t, devices, filepath.Join(dest, "devices"))
	}

	incrementalStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	if incrementalStats.Written >= firstStats.Written {
		t.Fatalf("incremental run unexpectedly not more efficient than first run: incremental wrote %d bytes, first wrote %d bytes", incrementalStats.Written, firstStats.Written)
	}

	// Make a change that is invisible with our current settings:
	// change the file contents without changing size and mtime.
	if err := os.WriteFile(hello, []byte("moon!"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(hello, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	// Replace the dest symlink to see if it will be restored
	if err := renameio.Symlink("wrong", filepath.Join(dest, "hey")); err != nil {
		t.Fatal(err)
	}

	if _, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout); err != nil {
		t.Fatal(err)
	}

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
	{
		got, err := os.Readlink(filepath.Join(dest, "hey"))
		if err != nil {
			t.Fatal(err)
		}
		want := "hello"
		if got != want {
			t.Fatalf("unexpected link target: got %q, want %q", got, want)
		}
	}
}
