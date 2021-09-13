package rsynctest

import (
	"io"
	"log"
	"net"
	"testing"

	"github.com/gokrazy/rsync/internal/anonssh"
	"github.com/gokrazy/rsync/internal/config"
	"github.com/gokrazy/rsync/internal/maincmd"
	"github.com/gokrazy/rsync/internal/rsyncd"
)

type TestServer struct {
	listeners []config.Listener

	// Port is the port on which the test server is listening on. Useful to pass
	// to rsync’s --port option.
	Port string
}

// InteropModMap is a convenience function to define an rsync module named
// “interop” with the specified path.
func InteropModMap(path string) map[string]config.Module {
	return map[string]config.Module{
		"interop": {
			Name: "interop",
			Path: path,
		},
	}
}

type Option func(ts *TestServer)

func Listeners(lns []config.Listener) Option {
	return func(ts *TestServer) {
		ts.listeners = lns
	}
}

func New(t *testing.T, modMap map[string]config.Module, opts ...Option) *TestServer {
	ts := &TestServer{}
	for _, opt := range opts {
		opt(ts)
	}
	if len(ts.listeners) == 0 {
		ts.listeners = []config.Listener{
			{Rsyncd: "localhost:0"},
		}
	}
	srv := &rsyncd.Server{
		Modules: modMap,
	}

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	log.Printf("listening on %s", ln.Addr())
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ts.Port = port

	if ts.listeners[0].AnonSSH != "" {
		cfg := &config.Config{
			ModuleMap: modMap,
		}
		go func() {
			err := anonssh.Serve(ln, cfg, func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
				return maincmd.Main(args, stdin, stdout, stderr, cfg)
			})
			if err != nil {
				log.Print(err)
			}
		}()
	} else {
		go srv.Serve(ln)
	}

	return ts
}
