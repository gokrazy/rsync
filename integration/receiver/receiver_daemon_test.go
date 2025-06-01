package receiver_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/internal/testlogger"
)

func TestDaemonReceiverSync(t *testing.T) {
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

	// start a server which receives data
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	rsync := exec.Command(rsyncBin,
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
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
		t.Fatal(err)
	}
}

// like TestDaemonReceiverSync, but specifying a destination path that is a subdirectory
// within the module.
func TestDaemonReceiverSyncSubdir(t *testing.T) {
	t.Parallel()

	rsyncBin := rsynctest.TridgeOrGTFO(t, "TODO: reason")

	tests := []struct {
		name        string
		destPath    string
		rsyncTarget string
	}{
		{
			name:        "simple subdir",
			destPath:    "destsubdir",
			rsyncTarget: "rsync://localhost/interop/destsubdir/",
		},
		{
			name:        "nested subdir",
			destPath:    "subdir/destsubdir",
			rsyncTarget: "rsync://localhost/interop/subdir/destsubdir/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			source := filepath.Join(tmp, "source")
			dest := filepath.Join(tmp, "dest")
			destLarge := filepath.Join(dest, tt.destPath, "large-data-file")

			headPattern := []byte{0x11}
			bodyPattern := []byte{0xbb}
			endPattern := []byte{0xee}
			rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

			// start a server which receives data
			srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

			rsync := exec.Command(rsyncBin,
				//		"--debug=all4",
				"--archive",
				// A verbosity level of 3 is enough, any higher than that and rsync
				// will start listing individual chunk matches.
				"-v", "-v", "-v", // "-v",
				"--port="+srv.Port,
				source+"/", // copy contents of source
				tt.rsyncTarget)
			rsync.Env = append(os.Environ(),
				// Ensure rsync does not localize decimal separators and fractional
				// points based on the current locale:
				"LANG=C.UTF-8")
			rsync.Stdout = testlogger.New(t)
			rsync.Stderr = testlogger.New(t)
			if err := rsync.Run(); err != nil {
				t.Fatalf("%v: %v", rsync.Args, err)
			}

			if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// like TestDaemonReceiverSync, but specifying a destination path that is a subdirectory
// within the module.
func TestDaemonReceiverSyncSubdirTraversal(t *testing.T) {
	t.Parallel()

	rsyncBin := rsynctest.TridgeOrGTFO(t, "TODO: reason")

	tests := []struct {
		name        string
		destPath    string
		rsyncTarget string
	}{
		{
			name:        "simple subdir",
			destPath:    "destsubdir",
			rsyncTarget: "rsync://localhost/interop/../",
		},
		{
			name:        "nested subdir",
			destPath:    "subdir/destsubdir",
			rsyncTarget: "rsync://localhost/interop/subdir/../../",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			source := filepath.Join(tmp, "source")
			dest := filepath.Join(tmp, "dest")
			destLarge := filepath.Join(dest, tt.destPath, "large-data-file")

			headPattern := []byte{0x11}
			bodyPattern := []byte{0xbb}
			endPattern := []byte{0xee}
			rsynctest.WriteLargeDataFile(t, source, headPattern, bodyPattern, endPattern)

			// start a server which receives data
			srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

			rsync := exec.Command(rsyncBin,
				//		"--debug=all4",
				"--archive",
				// A verbosity level of 3 is enough, any higher than that and rsync
				// will start listing individual chunk matches.
				"-v", "-v", "-v", // "-v",
				"--port="+srv.Port,
				source+"/", // copy contents of source
				tt.rsyncTarget)
			rsync.Env = append(os.Environ(),
				// Ensure rsync does not localize decimal separators and fractional
				// points based on the current locale:
				"LANG=C.UTF-8")
			var buf bytes.Buffer
			rsync.Stdout = &buf
			rsync.Stderr = &buf
			if err := rsync.Run(); err != nil {
				if strings.Contains(buf.String(), "path escapes from parent") {
					return
				}
				t.Fatalf("%v: %v", rsync.Args, err)
			}

			if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDaemonReceiverDelete(t *testing.T) {
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

	// start a server which receives data
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	run := func() {
		rsync := exec.Command(rsyncBin,
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
		rsync.Stdout = testlogger.New(t)
		rsync.Stderr = testlogger.New(t)
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

func TestDaemonReceiverSyncHardLinks(t *testing.T) {
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

	// start a server which receives data
	srv := rsynctest.New(t, rsynctest.WritableInteropModule(dest))

	rsync := exec.Command(rsyncBin,
		//		"--debug=all4",
		"--archive",
		"--hard-links",
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
	rsync.Stdout = &buf
	rsync.Stderr = &buf
	if err := rsync.Run(); err != nil {
		if strings.Contains(buf.String(), "gokr-rsync [receiver]: support for hard links not yet implemented") {
			return
		}
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
		t.Fatal(err)
	}
}
