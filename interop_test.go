package interop_test

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stapelberg/go-rsyncd-server/internal/rsyncd"
)

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
	{
		srv := &rsyncd.Server{}
		ln, err := net.Listen("tcp", "localhost:8730")
		if err != nil {
			t.Fatal(err)
		}
		log.Printf("listening on %s", ln.Addr())
		go srv.Serve(ln)
	}

	// {
	// 	config := filepath.Join(tmp, "rsyncd.conf")
	// 	rsyncdConfig := `
	// use chroot = no
	// # 0 = no limit
	// max connections = 0
	// pid file = ` + tmp + `/rsyncd.pid
	// exclude = lost+found/
	// transfer logging = yes
	// timeout = 900
	// ignore nonreadable = yes
	// dont compress   = *.gz *.tgz *.zip *.z *.Z *.rpm *.deb *.bz2 *.zst

	// [interop]
	//        path = ` + source + `
	//        comment = interop
	//        read only = yes
	//        list = true

	// `
	// 	if err := ioutil.WriteFile(config, []byte(rsyncdConfig), 0644); err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	srv := exec.Command("rsync",
	// 		"--daemon",
	// 		"--config="+config,
	// 		"--verbose",
	// 		"--address=localhost",
	// 		"--no-detach",
	// 		"--port=8730")
	// 	srv.Stdout = os.Stdout
	// 	srv.Stderr = os.Stderr
	// 	if err := srv.Start(); err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	go func() {
	// 		if err := srv.Wait(); err != nil {
	// 			t.Error(err)
	// 		}
	// 	}()
	// 	defer srv.Process.Kill()
	// }

	time.Sleep(1 * time.Second)

	// sync into dest dir
	rsync := exec.Command("/home/michael/src/openrsync/openrsync",
		//"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port=8730",
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
}
