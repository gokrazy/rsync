package rsync_test

import (
	"bytes"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gokrazy/rsync/internal/rsyncd"
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
	port := "8730"
	{
		srv := &rsyncd.Server{
			Modules: map[string]rsyncd.Module{
				"interop": rsyncd.Module{
					Path: nonExistant,
				},
			},
		}
		ln, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatal(err)
		}
		log.Printf("listening on %s", ln.Addr())
		_, port, err = net.SplitHostPort(ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		go srv.Serve(ln)
	}

	// TODO: verify error message when requesting a module that is not configured

	// sync into dest dir
	var buf bytes.Buffer
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+port,
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = &buf
	rsync.Stderr = &buf
	if err := rsync.Run(); err == nil {
		t.Fatalf("rsync unexpectedly did not return with an error exit code, output:\n%s", buf.String())
	}

	// got, err := ioutil.ReadFile(filepath.Join(dest, "dummy"))
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// if diff := cmp.Diff(want, got); diff != "" {
	// 	t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
	// }

	output := buf.String()
	if want := "no such file or directory"; !strings.Contains(output, want) {
		t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, output)
	}
	if want := nonExistant; !strings.Contains(output, want) {
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
	want := []byte("heyo")
	if err := ioutil.WriteFile(dummy, want, 0644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(source, "other")
	if err := ioutil.WriteFile(other, want, 0644); err != nil {
		t.Fatal(err)
	}

	// Remove read permission to trigger an error for one of the requested files.
	if err := os.Chmod(dummy, 0); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	port := "8730"
	{
		srv := &rsyncd.Server{
			Modules: map[string]rsyncd.Module{
				"interop": rsyncd.Module{
					Path: source,
				},
			},
		}
		ln, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatal(err)
		}
		log.Printf("listening on %s", ln.Addr())
		_, port, err = net.SplitHostPort(ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		go srv.Serve(ln)
	}

	// sync into dest dir
	var buf bytes.Buffer
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+port,
		"rsync://localhost/interop/", // copy contents of interop
		dest)                         // directly into dest
	rsync.Stdout = &buf
	rsync.Stderr = &buf
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if _, err := ioutil.ReadFile(filepath.Join(dest, "dummy")); err == nil {
		t.Fatalf("dummy file unexpectedly created in the destination")
	}

	got, err := ioutil.ReadFile(filepath.Join(dest, "other"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
	}
}
