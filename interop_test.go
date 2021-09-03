package rsync_test

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gokrazy/rsync/internal/rsyncd"
	"github.com/google/go-cmp/cmp"
)

// TODO: add a symbolic link and verify it

// TODO: test dry-run

// TODO: non-empty exclusion list

func TestInterop(t *testing.T) {
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

	// 	{
	// 		config := filepath.Join(tmp, "rsyncd.conf")
	// 		rsyncdConfig := `
	// 	use chroot = no
	// 	# 0 = no limit
	// 	max connections = 0
	// 	pid file = ` + tmp + `/rsyncd.pid
	// 	exclude = lost+found/
	// 	transfer logging = yes
	// 	timeout = 900
	// 	ignore nonreadable = yes
	// 	dont compress   = *.gz *.tgz *.zip *.z *.Z *.rpm *.deb *.bz2 *.zst

	// 	[interop]
	// 	       path = /home/michael/i3/docs
	// #` + source + `
	// 	       comment = interop
	// 	       read only = yes
	// 	       list = true

	// 	`
	// 		if err := ioutil.WriteFile(config, []byte(rsyncdConfig), 0644); err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		srv := exec.Command("rsync",
	// 			"--daemon",
	// 			"--config="+config,
	// 			"--verbose",
	// 			"--address=localhost",
	// 			"--no-detach",
	// 			"--port=8730")
	// 		srv.Stdout = os.Stdout
	// 		srv.Stderr = os.Stderr
	// 		if err := srv.Start(); err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		go func() {
	// 			if err := srv.Wait(); err != nil {
	// 				t.Error(err)
	// 			}
	// 		}()
	// 		defer srv.Process.Kill()
	// 	}

	time.Sleep(1 * time.Second)

	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		"--version")
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	// sync into dest dir
	rsync = exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+port,
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	got, err := ioutil.ReadFile(filepath.Join(dest, "dummy"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
	}

	// Run rsync again. This should not modify any files, but will result in
	// rsync sending sums to the sender.
	rsync = exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		// TODO: should this be --checksum instead?
		"--ignore-times", // disable rsync’s “quick check”
		"-v", "-v", "-v", "-v",
		"--port="+port,
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

}
