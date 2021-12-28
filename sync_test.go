package rsync_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/google/go-cmp/cmp"
	"github.com/stapelberg/rsyncparse"
)

func constructLargeDataFile(headPattern, bodyPattern, endPattern []byte) []byte {
	// create large data file in source directory to be copied
	head := bytes.Repeat(headPattern, 1*1024*1024)
	body := bytes.Repeat(bodyPattern, 1*1024*1024)
	end := bytes.Repeat(endPattern, 1*1024*1024)
	return append(append(head, body...), end...)
}

func writeLargeDataFile(t *testing.T, source string, headPattern, bodyPattern, endPattern []byte) {
	// create large data file in source directory to be copied
	content := constructLargeDataFile(headPattern, bodyPattern, endPattern)
	large := filepath.Join(source, "large-data-file")
	if err := os.MkdirAll(filepath.Dir(large), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(large, content, 0644); err != nil {
		t.Fatal(err)
	}
}

func dataFileMatches(fn string, headPattern, bodyPattern, endPattern []byte) error {
	want := constructLargeDataFile(headPattern, bodyPattern, endPattern)
	got, err := ioutil.ReadFile(fn)
	if err != nil {
		return err
	}
	if diff := cmp.Diff(want, got); diff != "" {
		return fmt.Errorf("unexpected file contents: diff (-want +got):\n%s", diff)
	}

	return nil
}

func TestSyncExtended(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	destLarge := filepath.Join(dest, "large-data-file")

	headPattern := []byte{0x11}
	bodyPattern := []byte{0xbb}
	endPattern := []byte{0xee}
	writeLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModMap(source))

	sync := func() *rsyncparse.Stats {
		rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
			//		"--debug=all4",
			"--archive",
			"-v", "-v", "-v", "-v",
			"--port="+srv.Port,
			"rsync://localhost/interop/", // copy contents of source
			dest)
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

		if err := dataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
			t.Fatal(err)
		}

		stats, err := rsyncparse.Parse(&buf)
		if err != nil {
			t.Fatal(err)
		}
		return stats
	}

	{
		// initial sync into dest dir
		stats := sync()
		if got, want := stats.TotalRead, int64(3*1024*1024); got < want {
			t.Fatalf("rsync unexpectedly did not read the whole file over the network: got %d, want >= %d", got, want)
		}
	}

	{
		// second sync (unmodified) into dest dir
		stats := sync()
		if got, want := stats.TotalRead, int64(512*1024); got >= want {
			t.Fatalf("rsync unexpectedly transferred more data than needed: got %d, want < %d", got, want)
		}
	}

	// Change the middle of the large data file:
	bodyPattern = []byte{0x66}
	{
		// modify the large data file
		writeLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

		// sync modifications into dest dir
		stats := sync()

		// TODO: verify speedup value, compare to rsync and openrsync

		if got, want := stats.TotalRead, int64(2*1024*1024); got >= want {
			t.Fatalf("rsync unexpectedly transferred more data than needed: got %d, want < %d", got, want)
		}
	}
}
