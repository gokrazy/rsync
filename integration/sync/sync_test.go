package sync_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/internal/testlogger"
)

func TestMain(m *testing.M) {
	rsynctest.CommandMain(m)
}

var statsTransferRe = regexp.MustCompile(`^sent ([0-9,]+) bytes  received ([0-9,]+) bytes  ([0-9,.]+) bytes/sec$`)

func TestSyncExtended(t *testing.T) {
	t.Parallel()

	rsyncBin := rsynctest.TridgeOrGTFO(t, "TODO: reason")

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

	sync := func() int64 {
		rsync := exec.Command(rsyncBin,
			//		"--debug=all4",
			"--archive",
			// A verbosity level of 3 is enough, any higher than that and rsync
			// will start listing individual chunk matches.
			"-v", "-v", "-v", // "-v",
			"--checksum",
			"--port="+srv.Port,
			"rsync://localhost/interop/", // copy contents of source
			dest)
		rsync.Env = append(os.Environ(),
			// Ensure rsync does not localize decimal separators and fractional
			// points based on the current locale:
			"LANG=C.UTF-8")
		var buf bytes.Buffer
		rsync.Stdout = &buf
		rsync.Stderr = testlogger.New(t)
		if err := rsync.Run(); err != nil {
			t.Fatalf("%v: %v", rsync.Args, err)
		}

		if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
			t.Fatal(err)
		}

		totalRead := int64(-1)
		for _, line := range strings.Split(buf.String(), "\n") {
			// log.Printf("rsync output line: %q", line)
			if strings.HasPrefix(line, "sent ") {
				// e.g.:
				// sent 1,590 bytes  received 18 bytes  3,216.00 bytes/sec
				// total size is 1,188,046  speedup is 738.83
				matches := statsTransferRe.FindStringSubmatch(line)
				if len(matches) == 0 {
					t.Fatalf("could not parse rsync 'sent' line")
				}

				var err error
				totalRead, err = strconv.ParseInt(strings.ReplaceAll(matches[2], ",", ""), 0, 64)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		return totalRead
	}

	{
		// initial sync into dest dir
		totalRead := sync()
		if got, want := totalRead, int64(3*1024); got < want {
			t.Fatalf("rsync unexpectedly did not read the whole file over the network: got %d, want >= %d", got, want)
		}
	}

	{
		// second sync (unmodified) into dest dir
		totalRead := sync()
		if got, want := totalRead, int64(512*1024); got >= want {
			t.Fatalf("rsync unexpectedly transferred more data than needed: got %d, want < %d", got, want)
		}
	}

	// Change the middle of the large data file:
	bodyPattern = []byte{0x66}
	{
		// modify the large data file
		rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

		// sync modifications into dest dir
		totalRead := sync()

		// TODO: verify speedup value, compare to rsync and openrsync

		if got, want := totalRead, int64(2*1024*1024); got >= want {
			t.Fatalf("rsync unexpectedly transferred more data than needed: got %d, want < %d", got, want)
		}
	}
}

func TestSyncMultipleSources(t *testing.T) {
	tmp := t.TempDir()
	src1 := filepath.Join(tmp, "src1")
	src2 := filepath.Join(tmp, "src2")
	dest := filepath.Join(tmp, "dest")
	if err := os.MkdirAll(src1, 0755); err != nil {
		t.Fatal(err)
	}
	const hello = "world"
	if err := os.WriteFile(filepath.Join(src1, "hello"), []byte(hello), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(src2, 0755); err != nil {
		t.Fatal(err)
	}
	const bye = "moon"
	if err := os.WriteFile(filepath.Join(src2, "bye"), []byte(bye), 0644); err != nil {
		t.Fatal(err)
	}

	rsynctest.Run(t, "gokr-rsync",
		"-av",
		src1,
		src2,
		dest)

	hellob, err := os.ReadFile(filepath.Join(dest, "src1", "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hellob, []byte(hello)) {
		t.Errorf("file 'hello' has unexpected contents: got %q, want %q", string(hellob), hello)
	}

	byeb, err := os.ReadFile(filepath.Join(dest, "src2", "bye"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(byeb, []byte(bye)) {
		t.Errorf("file 'bye' has unexpected contents: got %q, want %q", string(byeb), bye)
	}
}
