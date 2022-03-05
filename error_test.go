package rsync_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/google/go-cmp/cmp"
)

func TestErrors(t *testing.T) {
	tmp := t.TempDir()

	dest := filepath.Join(tmp, "dest")

	// We configure an rsync module with a non-existant path to trigger an
	// error. Removing read permission from a file is not sufficient because
	// that does not actually trigger an error! See TestNoReadPermission.
	nonExistant := filepath.Join(tmp, "non/existant")

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(nonExistant))

	// sync into dest dir
	var buf bytes.Buffer
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = &buf
	rsync.Stderr = &buf
	if err := rsync.Run(); err == nil {
		t.Fatalf("rsync unexpectedly did not return with an error exit code, output:\n%s", buf.String())
	}

	output := buf.String()
	if want := "no such file or directory"; !strings.Contains(output, want) {
		t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, output)
	}
	if want := nonExistant; !strings.Contains(output, want) {
		t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, output)
	}
}

func TestNoSuchModule(t *testing.T) {
	tmp := t.TempDir()

	dest := filepath.Join(tmp, "dest")

	// start a server to sync from
	srv := rsynctest.New(t, nil)

	// sync into dest dir
	var buf bytes.Buffer
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost/requesting-nonsense/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = &buf
	rsync.Stderr = &buf
	if err := rsync.Run(); err == nil {
		t.Fatalf("rsync unexpectedly did not return with an error exit code, output:\n%s", buf.String())
	}

	output := buf.String()
	if want := "Unknown module"; !strings.Contains(output, want) {
		t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, output)
	}
}

func TestNoReadPermission(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	// create files in source to be copied
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	dummy := filepath.Join(source, "dummy")
	if err := ioutil.WriteFile(dummy, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(source, "other")
	want := []byte("other file contents")
	if err := ioutil.WriteFile(other, want, 0644); err != nil {
		t.Fatal(err)
	}

	// Remove read permission to trigger an error for one of the requested files.
	if err := os.Chmod(dummy, 0); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	// sync into dest dir
	var buf bytes.Buffer
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost/interop/", // copy contents of interop
		dest)                         // directly into dest
	rsync.Stdout = &buf
	rsync.Stderr = &buf
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if os.Getuid() > 0 {
		// uid 0 can read the file despite chmod(0), so skip this check:

		if _, err := ioutil.ReadFile(filepath.Join(dest, "dummy")); err == nil {
			t.Fatalf("dummy file unexpectedly created in the destination")
		}
	}

	got, err := ioutil.ReadFile(filepath.Join(dest, "other"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
	}
}
