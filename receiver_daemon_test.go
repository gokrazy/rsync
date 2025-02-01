package rsync_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gokrazy/rsync/internal/rsynctest"
)

func TestDaemonReceiverSync(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	destLarge := filepath.Join(dest, "large-data-file")

	headPattern := []byte{0x11}
	bodyPattern := []byte{0xbb}
	endPattern := []byte{0xee}
	rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

	// start a server which receives data
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		// A verbosity level of 3 is enough, any higher than that and rsync
		// will start listing individual chunk matches.
		"-v", "-v", "-v", // "-v",
		"--port="+srv.Port,
		source+"/", // copy contents of source
		"rsync://localhost/interop/")
	rsync.Env = append(os.Environ(),
		// Ensure rsync does not localize decimal separators and fractional
		// points based on the current locale:
		"LANG=C.UTF-8")
	var buf bytes.Buffer
	rsync.Stdout = io.MultiWriter(&buf, os.Stdout)
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonReceiverDelete(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	destLarge := filepath.Join(dest, "large-data-file")

	headPattern := []byte{0x11}
	bodyPattern := []byte{0xbb}
	endPattern := []byte{0xee}
	rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

	// start a server which receives data
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	run := func() {
		rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
			//		"--debug=all4",
			"--archive",
			"--delete",
			// A verbosity level of 3 is enough, any higher than that and rsync
			// will start listing individual chunk matches.
			"-v", "-v", "-v", // "-v",
			"--port="+srv.Port,
			source+"/", // copy contents of source
			"rsync://localhost/interop/")
		rsync.Env = append(os.Environ(),
			// Ensure rsync does not localize decimal separators and fractional
			// points based on the current locale:
			"LANG=C.UTF-8")
		var buf bytes.Buffer
		rsync.Stdout = io.MultiWriter(&buf, os.Stdout)
		rsync.Stderr = os.Stderr
		if err := rsync.Run(); err != nil {
			t.Fatalf("%v: %v", rsync.Args, err)
		}
	}
	run()

	if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
		t.Fatal(err)
	}

	// Add more files to the destination, which should be deleted:
	extra := filepath.Join(dest, "extrafile")
	if err := os.WriteFile(extra, []byte("deleteme"), 0644); err != nil {
		t.Fatal(err)
	}
	run()
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Errorf("expected %s to be deleted, but it still exists", extra)
	}
}
