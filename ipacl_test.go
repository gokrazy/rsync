package rsync_test

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/internal/rsynctest"
)

type connWithRemoteAddrListener struct {
	net.Listener

	remoteAddr net.Addr
}

func (l *connWithRemoteAddrListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &connWithRemoteAddr{
		Conn:       conn,
		remoteAddr: l.remoteAddr,
	}, nil
}

type connWithRemoteAddr struct {
	net.Conn

	remoteAddr net.Addr
}

func (c *connWithRemoteAddr) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func TestIPACL(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")
	// create files in source to be copied
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}

	cfg, err := rsyncdconfig.FromString(`
[[module]]
name = "interop"
path = "` + source + `"
acl = [
  "allow 192.168.1.0/24",
  "allow 2001:db8::1/32",
  "deny all"
]

`)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		remoteAddr string
		wantError  bool
	}{
		{
			remoteAddr: "192.168.1.1",
			wantError:  false,
		},
		{
			remoteAddr: "2001:db8::1234",
			wantError:  false,
		},
		{
			remoteAddr: "10.0.0.1",
			wantError:  true,
		},
	} {
		t.Run(tt.remoteAddr, func(t *testing.T) {
			ln, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatal(err)
			}
			ln = &connWithRemoteAddrListener{
				Listener: ln,
				remoteAddr: &net.TCPAddr{
					IP:   net.ParseIP(tt.remoteAddr),
					Port: 1234,
				},
			}
			srv := rsynctest.New(t, cfg.Modules, rsynctest.Listener(ln))

			// request module list: this should work regardless of the source IP
			var buf bytes.Buffer
			rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"--port="+srv.Port,
				"rsync://localhost")
			rsync.Stdout = &buf
			rsync.Stderr = os.Stderr
			if err := rsync.Run(); err != nil {
				t.Fatalf("%v: %v", rsync.Args, err)
			}

			output := buf.String()
			if want := "interop\tinterop"; !strings.Contains(output, want) {
				t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, output)
			}

			buf.Reset()
			// actually transfer the interop module (in dry-run mode) to trigger
			// the ACL check dry run (slight differences in protocol)
			rsync = exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"--port="+srv.Port,
				"--dry-run",
				"rsync://localhost/interop/", // copy contents of interop
				//source+"/", // sync from local directory
				dest) // directly into dest
			rsync.Stdout = os.Stdout
			rsync.Stderr = &buf
			if err := rsync.Run(); err != nil {
				if !tt.wantError {
					t.Fatalf("%v: %v", rsync.Args, err)
				}
			}

			if tt.wantError {
				if !strings.Contains(buf.String(), "@ERROR: access denied") {
					t.Fatalf("expected access denied error, got: %q", buf.String())
				}
			}
		})
	}
}
