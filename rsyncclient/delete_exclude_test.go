package rsyncclient_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/internal/testlogger"
	"github.com/gokrazy/rsync/rsyncclient"
)

// TestClientSenderDeleteExclude exercises the client as the sender (push) with
// --delete and a wildcard --exclude against a real rsync --server receiver.
func TestClientSenderDeleteExclude(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dest := filepath.Join(tmp, "dest")
	for _, d := range []string{src, dest} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Source: one file to keep, one to exclude by wildcard.
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "drop.log"), []byte("drop"), 0644); err != nil {
		t.Fatal(err)
	}
	// Dest: a pre-existing extraneous file that --delete must prune.
	if err := os.WriteFile(filepath.Join(dest, "stale.txt"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	client, err := rsyncclient.New(
		[]string{"-a", "--delete", "--exclude=*.log"},
		rsyncclient.WithSender(),
		rsyncclient.WithStderr(testlogger.New(t)),
	)
	if err != nil {
		t.Fatal(err)
	}

	rsync := exec.Command(rsynctest.AnyRsync(t), client.ServerCommandOptions(dest)...)
	wc, err := rsync.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	rc, err := rsync.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Start(); err != nil {
		t.Fatal(err)
	}
	conn := &struct {
		io.Reader
		io.Writer
	}{Reader: rc, Writer: wc}
	if _, err := client.Run(t.Context(), conn, []string{src + "/"}); err != nil {
		t.Fatal(err)
	}
	wc.Close()
	if err := rsync.Wait(); err != nil {
		t.Fatalf("rsync server: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "keep.txt")); err != nil {
		t.Errorf("keep.txt should have been synced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "drop.log")); !os.IsNotExist(err) {
		t.Errorf("drop.log should have been excluded, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale.txt should have been deleted by --delete, stat err=%v", err)
	}
}
