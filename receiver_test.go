package rsync_test

import (
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/receivermaincmd"
	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/google/go-cmp/cmp"
	"github.com/google/renameio/v2"
)

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
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(source, "hello")
	if err := ioutil.WriteFile(hello, []byte("world"), 0644); err != nil {
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
	if err := ioutil.WriteFile(no, []byte("no"), 0666); err != nil {
		t.Fatal(err)
	}
	uid, gid, verifyUid := setUid(t, no)

	devices := filepath.Join(source, "devices")
	if os.Getuid() == 0 {
		rsynctest.CreateDummyDeviceFiles(t, devices)
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	args := []string{
		"gokr-rsync",
		"-aH",
		"rsync://localhost:" + srv.Port + "/interop/",
		dest,
	}
	firstStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}

	{
		want := []byte("world")
		got, err := ioutil.ReadFile(filepath.Join(dest, "hello"))
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
	if err := ioutil.WriteFile(hello, []byte("moon!"), 0644); err != nil {
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
		got, err := ioutil.ReadFile(filepath.Join(dest, "hello"))
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
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	destLarge := filepath.Join(dest, "large-data-file")

	headPattern := []byte{0x11}
	bodyPattern := []byte{0xbb}
	endPattern := []byte{0xee}
	rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	args := []string{
		"gokr-rsync",
		"-aH",
		"rsync://localhost:" + srv.Port + "/interop/",
		dest,
	}
	firstStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("firstStats: %+v", firstStats)
	//     receiver_test.go:211: firstStats: &{Read:91 Written:3146087 Size:3149824}

	if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
		t.Fatal(err)
	}

	// Change the middle of the large data file:
	bodyPattern = []byte{0x66}
	// modify the large data file
	rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

	incrementalStats, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("incrementalStats: %+v", incrementalStats)
	if got, want := incrementalStats.Written, int64(2*1024*1024); got >= want {
		t.Fatalf("rsync unexpectedly transferred more data than needed: got %d, want < %d", got, want)
	}
}

func TestReceiverSSH(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(source, "hello"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	srv := rsynctest.New(t,
		rsynctest.InteropModule(source),
		rsynctest.Listeners([]rsyncdconfig.Listener{
			{AnonSSH: "localhost:0"},
		}))

	// ensure the user running the tests (root when doing the privileged run!)
	// has an SSH private key:
	privKeyPath := filepath.Join(tmp, "ssh_private_key")
	genKey := exec.Command("ssh-keygen",
		"-N", "",
		"-t", "ed25519",
		"-f", privKeyPath)
	genKey.Stdout = os.Stdout
	genKey.Stderr = os.Stderr
	if err := genKey.Run(); err != nil {
		t.Fatalf("%v: %v", genKey.Args, err)
	}

	// sync into dest dir
	args := []string{
		"gokr-rsync",
		"-aH",
		"-e", "ssh -vv -o IdentityFile=" + privKeyPath + " -o StrictHostKeyChecking=no -o CheckHostIP=no -o UserKnownHostsFile=/dev/null -p " + srv.Port,
		"rsync://localhost/interop/",
		dest,
	}
	if _, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout); err != nil {
		t.Fatal(err)
	}

	{
		want := []byte("world")
		got, err := ioutil.ReadFile(filepath.Join(dest, "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
}

func TestReceiverCommand(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(source, "hello"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	// // sync into dest dir
	// rsync := exec.Command("rsync", //*/ "/home/michael/src/openrsync/openrsync",
	// 	append(
	// 		append([]string{
	// 			//		"--debug=all4",
	// 			"--archive",
	// 			"--protocol=27",
	// 			"-v", "-v", "-v", "-v",
	// 			"-e", os.Args[0],
	// 		}, sourcesArgs...),
	// 		dest)...)
	// rsync.Stdout = os.Stdout
	// rsync.Stderr = os.Stderr
	// if err := rsync.Run(); err != nil {
	// 	t.Fatalf("%v: %v", rsync.Args, err)
	// }

	// sync into dest dir
	args := []string{
		"gokr-rsync",
		"-aH",
		"-e", os.Args[0],
		"localhost:" + source + "/",
		dest,
	}
	if _, err := receivermaincmd.Main(args, os.Stdin, os.Stdout, os.Stdout); err != nil {
		t.Fatal(err)
	}

	{
		want := []byte("world")
		got, err := ioutil.ReadFile(filepath.Join(dest, "hello"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
}
