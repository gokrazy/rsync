package rsynctest

import (
	"log"
	"net"
	"testing"

	"github.com/gokrazy/rsync/internal/config"
	"github.com/gokrazy/rsync/internal/rsyncd"
)

type TestServer struct {
	// Port is the port on which the test server is listening on. Useful to pass
	// to rsync’s --port option.
	Port string
}

// InteropModMap is a convenience function to define an rsync module named
// “interop” with the specified path.
func InteropModMap(path string) map[string]config.Module {
	return map[string]config.Module{
		"interop": config.Module{
			Name: "interop",
			Path: path,
		},
	}
}

func New(t *testing.T, modMap map[string]config.Module) *TestServer {
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

	go srv.Serve(ln)

	return &TestServer{
		Port: port,
	}
}
