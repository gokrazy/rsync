package receiver_test

import (
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/rsyncd"
	"github.com/google/go-cmp/cmp"
	"github.com/google/renameio/v2"
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

func TestReceiver(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	sourcesub := filepath.Join(source, "subdir")
	if err := os.MkdirAll(sourcesub, 0755); err != nil {
		t.Fatal(err)
	}
	sourcesubempty := filepath.Join(source, "subdirempty")
	if err := os.MkdirAll(sourcesubempty, 0755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(source, "hello")
	if err := os.WriteFile(hello, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	hellosub := filepath.Join(sourcesub, "hello")
	if err := os.WriteFile(hellosub, []byte("space"), 0644); err != nil {
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
	if err := os.Chtimes(hellosub, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sourcesubempty, mtime, mtime); err != nil {
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
	srv := rsynctest.NewInMemory(t, rsyncd.Module{
		Name: "interop",
		Path: source,
	})
	args := []string{"-aH"}
	firstStats := srv.RunClient(t, args, []string{dest})

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
	{
		st, err := os.Stat(filepath.Join(dest, "subdir", "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := st.ModTime(), mtime; !got.Equal(want) {
			t.Errorf("dest/subdir/hello has unexpected mod time: got %v, want %v", got, want)
		}
	}
	{
		st, err := os.Stat(filepath.Join(dest, "subdirempty"))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := st.ModTime(), mtime; !got.Equal(want) {
			t.Errorf("dest/subdirempty has unexpected mod time: got %v, want %v", got, want)
		}
	}
	if os.Getuid() == 0 {
		rsynctest.VerifyDummyDeviceFiles(t, devices, filepath.Join(dest, "devices"))
	}

	incrementalStats := srv.RunClient(t, args, []string{dest})
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

	srv.RunClient(t, args, []string{dest})

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

func TestReceiverSync(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	destLarge := filepath.Join(dest, "large-data-file")

	headPattern := []byte{0x11}
	bodyPattern := []byte{0xbb}
	endPattern := []byte{0xee}
	rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

	// start a server to sync from
	srv := rsynctest.NewInMemory(t, rsyncd.Module{
		Name: "interop",
		Path: source,
	})
	args := []string{"-aH"}
	firstStats := srv.RunClient(t, args, []string{dest})
	t.Logf("firstStats: %+v", firstStats)
	//     receiver_test.go:211: firstStats: &{Read:91 Written:3146087 Size:3149824}

	if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
		t.Fatal(err)
	}

	// Change the middle of the large data file:
	bodyPattern = []byte{0x66}
	// modify the large data file
	rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

	incrementalStats := srv.RunClient(t, args, []string{dest})
	t.Logf("incrementalStats: %+v", incrementalStats)
	if got, want := incrementalStats.Written, int64(2*1024*1024); got >= want {
		t.Fatalf("rsync unexpectedly transferred more data than needed: got %d, want < %d", got, want)
	}
}

func TestReceiverSyncDelete(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	destLarge := filepath.Join(dest, "large-data-file")

	headPattern := []byte{0x11}
	bodyPattern := []byte{0xbb}
	endPattern := []byte{0xee}
	rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

	// start a server to sync from
	srv := rsynctest.NewInMemory(t, rsyncd.Module{
		Name: "interop",
		Path: source,
	})
	args := []string{"-aH", "--delete"}
	firstStats := srv.RunClient(t, args, []string{dest})
	t.Logf("firstStats: %+v", firstStats)
	//     receiver_test.go:211: firstStats: &{Read:91 Written:3146087 Size:3149824}

	if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
		t.Fatal(err)
	}

	// Add more files to the destination, which should be deleted:
	extra := filepath.Join(dest, "extrafile")
	if err := os.WriteFile(extra, []byte("deleteme"), 0644); err != nil {
		t.Fatal(err)
	}
	extraDir := filepath.Join(dest, "extradir")
	if err := os.MkdirAll(extraDir, 0755); err != nil {
		t.Fatal(err)
	}
	extra2 := filepath.Join(extraDir, "blocker")
	if err := os.WriteFile(extra2, []byte("deleteme"), 0644); err != nil {
		t.Fatal(err)
	}

	srv.RunClient(t, args, []string{dest})
	for _, gone := range []string{extra, extraDir, extra2} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", gone)
		}
	}
}

func TestReceiverSSH(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "hello"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	srv := rsynctest.New(t,
		rsynctest.InteropModule(source),
		rsynctest.Listeners([]rsyncdconfig.Listener{
			{
				AnonSSH:     "localhost:0",
				HostKeyPath: filepath.Join(tmp, "ssh_host_ed25519_key"),
			},
		}))

	// ensure the user running the tests (root when doing the privileged run!)
	// has an SSH private key:
	privKeyPath := filepath.Join(tmp, "ssh_private_key")
	if err := genKey(privKeyPath); err != nil {
		t.Fatal(err)
	}

	// Ensure SSH_* environment variables (like SSH_ASKPASS or
	// SSH_AUTH_SOCK) do not leak into the test, otherwise tests
	// might be interrupted by a text UI password prompt.
	t.Setenv("SSH_ASKPASS", "")
	t.Setenv("SSH_AUTH_SOCK", "")
	// sync into dest dir
	rsynctest.Run(t, "gokr-rsync",
		"-aH",
		"--dry-run",
		"-e", "ssh -vv -o IdentityFile="+privKeyPath+" -o StrictHostKeyChecking=no -o CheckHostIP=no -o UserKnownHostsFile=/dev/null -p "+srv.Port,
		"rsync://localhost/interop/",
		dest)
}

func TestReceiverCommand(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "hello"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	// sync into dest dir
	rsynctest.Run(t, "gokr-rsync",
		"-aH",
		"--dry-run",
		"-e", os.Args[0],
		"localhost:"+source+"/",
		dest)
}

// TestReceiverSymlinkTraversal passes by default but is useful to simulate
// a symlink race TOCTOU attack by modifying rsyncd/rsyncd.go.
func TestReceiverSymlinkTraversal(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "passwd"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(source, "passwd")
	if err := os.WriteFile(hello, []byte("benign"), 0644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(source, "dir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	subhello := filepath.Join(subdir, "passwd")
	if err := os.WriteFile(subhello, []byte("benign"), 0644); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	rsynctest.Run(t, "gokr-rsync",
		"-aH",
		"rsync://localhost:"+srv.Port+"/interop/",
		dest)

	{
		want := []byte("benign")
		got, err := os.ReadFile(filepath.Join(dest, "passwd"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}

	{
		want := []byte("benign")
		got, err := os.ReadFile(filepath.Join(dest, "dir", "passwd"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
}
