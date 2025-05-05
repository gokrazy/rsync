package receiver_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/google/go-cmp/cmp"
)

func init() {
	// Run this test in UTC so that the printed timestamp matches our expected
	// output.
	time.Local = time.UTC
}

func TestReceiverListing(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	// This explicit chmod might seem redundant at first,
	// but is required to make the test pass with umask 027.
	if err := os.Chmod(source, 0755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(source, "hello")
	if err := os.WriteFile(hello, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	// This explicit chmod might seem redundant at first,
	// but is required to make the test pass with umask 027.
	if err := os.Chmod(hello, 0644); err != nil {
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

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	stdout, _ := rsynctest.Output(t, "gokr-rsync",
		"-aH",
		"rsync://localhost:"+srv.Port+"/interop/")
	want := `drwxr-xr-x        4096 2009/11/10 23:00:00 .
-rw-r--r--           5 2009/11/10 23:00:00 hello
`
	if diff := cmp.Diff(want, string(stdout)); diff != "" {
		t.Fatalf("unexpected listing: diff (-want +got):\n%s", diff)
	}
}
