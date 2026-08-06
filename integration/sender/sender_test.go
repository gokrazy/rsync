package sender_test

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/internal/testlogger"
	"github.com/google/go-cmp/cmp"
)

func TestMain(m *testing.M) {
	if err := rsynctest.CommandMain(m); err != nil {
		log.Fatal(err)
	}
}

func setUid(t *testing.T, fn string) (uid, gid int, verify bool) {
	if os.Getuid() != 0 {
		return 0, 0, false
	}

	u, err := user.Lookup("nobody")
	if err != nil {
		t.Fatal(err)
	}

	uid64, err := strconv.ParseInt(u.Uid, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	uid = int(uid64)

	gid64, err := strconv.ParseInt(u.Gid, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	gid = int(gid64)

	if err := os.Chown(fn, uid, gid); err != nil {
		t.Fatal(err)
	}

	return uid, gid, true
}

func TestSender(t *testing.T) {
	t.Parallel()

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

	// start a server to sync to
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	args := []string{
		"gokr-rsync",
		"-aH",
		source + "/",
		"rsync://localhost:" + srv.Port + "/interop/",
	}
	firstStats := rsynctest.Run(t, args...)

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
		gotUID, gotGID := rsynctest.StatUidGid(t, st)
		if got, want := gotUID, uid; got != want {
			t.Errorf("unexpected uid: got %d, want %d", got, want)
		}
		if got, want := gotGID, gid; got != want {
			t.Errorf("unexpected gid: got %d, want %d", got, want)
		}
	}
	if os.Getuid() == 0 {
		rsynctest.VerifyDummyDeviceFiles(t, devices, filepath.Join(dest, "devices"))
	}

	incrementalStats := rsynctest.Run(t, args...)
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
	rsynctest.ReplaceSymlink(t, "wrong", filepath.Join(dest, "hey"))

	rsynctest.Run(t, args...)

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
	t.Parallel()

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

	// start a server to sync to
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	args := []string{
		"gokr-rsync",
		"-aH",
		source,
		"rsync://localhost:" + srv.Port + "/interop/",
	}
	firstStats := rsynctest.Run(t, args...)

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
		gotUID, gotGID := rsynctest.StatUidGid(t, st)
		if got, want := gotUID, uid; got != want {
			t.Errorf("unexpected uid: got %d, want %d", got, want)
		}
		if got, want := gotGID, gid; got != want {
			t.Errorf("unexpected gid: got %d, want %d", got, want)
		}
	}
	if os.Getuid() == 0 {
		rsynctest.VerifyDummyDeviceFiles(t, devices, filepath.Join(dest, "devices"))
	}

	incrementalStats := rsynctest.Run(t, args...)
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
	rsynctest.ReplaceSymlink(t, "wrong", filepath.Join(dest, "hey"))

	rsynctest.Run(t, args...)

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

// like TestSender, but using a relative path
func TestSenderRelative(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(source, "hello")
	if err := os.WriteFile(hello, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	// start a server to sync to
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	args := []string{
		"gokr-rsync",
		"-aH",
		"source",
		"rsync://localhost:" + srv.Port + "/interop/",
	}
	rsynctest.Run(t, args...)

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
}

// TestSenderNamedSourceParentUnreadable is a regression test for
// https://github.com/gokrazy/rsync/issues/66: syncing a named directory (no
// trailing slash) must not require access to the source’s parent directory.
//
// The sender allowlists the source itself for Landlock, but not its parent, so
// opening the parent (as the pre-fix code did to obtain the source basename)
// is denied. We reproduce that portably — without Landlock — by removing read
// access from the parent while keeping search (execute) access, which likewise
// makes os.OpenRoot(parent) fail but os.OpenRoot(source) succeed.
func TestSenderNamedSourceParentUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions do not gate reads on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permission checks, so the parent would still be readable")
	}
	t.Parallel()

	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	source := filepath.Join(parent, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "hello"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	// Remove read (but keep search) on the parent, then restore it before the
	// t.TempDir() cleanup so RemoveAll can recurse back in.
	if err := os.Chmod(parent, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o755) })

	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	// Named directory: no trailing slash, so the source directory itself is
	// copied and the receiver creates dest/source/hello.
	//
	// --gokr.dont_restrict: the unreadable parent (not Landlock) provides
	// the constraint under test; skipping the Landlock ruleset stays within
	// the kernel's limit of 16 stacked rulesets per process (all tests in
	// this package share one process).
	rsynctest.Run(t, "gokr-rsync", "-aH", "--gokr.dont_restrict", source, "rsync://localhost:"+srv.Port+"/interop/")

	want := []byte("world")
	got, err := os.ReadFile(filepath.Join(dest, "source", "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
	}
}

// TestSenderNamedFileParentUnreadable is the single-file variant of
// TestSenderNamedSourceParentUnreadable: syncing a named file must not
// require access to the file's parent directory either (issue #66; the
// client allowlists the file itself as a read-only Landlock rule).
func TestSenderNamedFileParentUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions do not gate reads on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permission checks, so the parent would still be readable")
	}
	t.Parallel()

	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	hello := filepath.Join(parent, "hello")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hello, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	// Remove read (but keep search) on the parent, then restore it before the
	// t.TempDir() cleanup so RemoveAll can recurse back in.
	if err := os.Chmod(parent, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o755) })

	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	// --gokr.dont_restrict: see TestSenderNamedSourceParentUnreadable.
	rsynctest.Run(t, "gokr-rsync", "-aH", "--gokr.dont_restrict", hello, "rsync://localhost:"+srv.Port+"/interop/")

	want := []byte("world")
	got, err := os.ReadFile(filepath.Join(dest, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
	}
}

func TestSenderTraversal(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "hello.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "passwd"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	args := []string{
		"gokr-rsync",
		"-aH",
		"rsync://localhost:" + srv.Port + "/interop/../",
		dest + "/",
	}
	rsynctest.Run(t, args...)

	passwd := filepath.Join(dest, "passwd")

	got, err := os.ReadFile(passwd)
	if err == nil {
		t.Fatalf("unexpectedly synced /etc/passwd: %s", string(got))
	}
}

// like TestSender, but both source and dest are local directories
func TestSenderBothLocal(t *testing.T) {
	t.Parallel()

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
	firstStats := rsynctest.Run(t, args...)

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
		gotUID, gotGID := rsynctest.StatUidGid(t, st)
		if got, want := gotUID, uid; got != want {
			t.Errorf("unexpected uid: got %d, want %d", got, want)
		}
		if got, want := gotGID, gid; got != want {
			t.Errorf("unexpected gid: got %d, want %d", got, want)
		}
	}
	if os.Getuid() == 0 {
		rsynctest.VerifyDummyDeviceFiles(t, devices, filepath.Join(dest, "devices"))
	}

	incrementalStats := rsynctest.Run(t, args...)
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
	rsynctest.ReplaceSymlink(t, "wrong", filepath.Join(dest, "hey"))

	rsynctest.Run(t, args...)

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

// like TestSender, but the source is a single regular file, not a directory
func TestSenderBothLocalFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.WriteFile(source, []byte("hey"), 0644); err != nil {
		t.Fatal(err)
	}
	mtime, err := time.Parse(time.RFC3339, "2009-11-10T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"gokr-rsync",
		"-avH",
		source,
		dest,
	}
	rsynctest.Run(t, args...)

	{
		want := []byte("hey")
		got, err := os.ReadFile(filepath.Join(dest, "source"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
}

// like TestSenderBothLocalFile, but with an invocation that once caused a hang
// (see issue #43).
func TestSenderBothLocalHang(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}

	hello := filepath.Join(source, "hello.txt")

	if err := os.WriteFile(hello, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"gokr-rsync",
		"-rv",
		source,
		dest,
	}
	rsynctest.Run(t, args...)

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "source", "hello.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
}

// like TestSenderBothLocalFile, but with an invocation that used to fail
// with error message "file has changed mid-transfer" (issue #53).
func TestSenderPartial257K(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	want := make([]byte, 1024*257+1)

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}

	hello := filepath.Join(source, "hello.txt")

	if err := os.WriteFile(hello, want, 0644); err != nil {
		t.Fatal(err)
	}

	destFile := filepath.Join(dest, "source", "hello.txt")
	if err := os.MkdirAll(filepath.Dir(destFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destFile, []byte{0}, 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"gokr-rsync",
		"-rv",
		source,
		dest,
	}
	rsynctest.Run(t, args...)

	{
		got, err := os.ReadFile(destFile)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
}

func TestReceiverCommandDryRun(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("stdin not supported on Windows")
	}

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
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	hello = filepath.Join(dest, "hello")
	if err := os.WriteFile(hello, []byte("moon"), 0644); err != nil {
		t.Fatal(err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rsync := exec.Command(rsynctest.AnyRsync(t),
		"--dry-run",
		"-e", `"`+exe+`"`,
		"-a",
		filepath.Base(source)+"/",
		"localhost:"+dest+"/")
	rsync.Dir = filepath.Dir(source)
	rsync.Stdout = &buf
	rsync.Stderr = testlogger.New(t)
	t.Logf("%v", rsync.Args)
	if err := rsync.Run(); err != nil {
		t.Fatalf("rsync error, output:\n%s", buf.String())
	}
}
