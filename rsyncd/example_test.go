package rsyncd_test

import (
	"context"
	"log"
	"net"
	"testing/fstest"

	"github.com/gokrazy/rsync/rsyncd"
)

func ExampleNewServer() {
	listener, err := net.Listen("tcp", "localhost:8873")
	if err != nil {
		log.Fatal(err)
	}

	rsyncServer, err := rsyncd.NewServer([]rsyncd.Module{
		{
			Name: "music",
			Path: "/home/bob/Music",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := rsyncServer.Serve(context.Background(), listener); err != nil {
		log.Fatal(err)
	}
}

// ExampleNewServer_fsFS shows how to serve an rsync module backed by an [fs.FS]
// instead of a filesystem path. This allows serving files from in-memory
// filesystems, embedded files, or any other [fs.FS] implementation.
func ExampleNewServer_fsFS() {
	// Create an in-memory filesystem using testing/fstest.
	// Any fs.FS implementation works (embed.FS, zip archives, etc.)
	memfs := fstest.MapFS{
		"hello.txt": &fstest.MapFile{
			Data: []byte("Hello from fs.FS!"),
			Mode: 0o644,
		},
		"readme.md": &fstest.MapFile{
			Data: []byte("# Welcome\nThis is served from memory."),
			Mode: 0o644,
		},
	}

	listener, err := net.Listen("tcp", "localhost:8873")
	if err != nil {
		log.Fatal(err)
	}

	rsyncServer, err := rsyncd.NewServer([]rsyncd.Module{
		{
			Name: "inmemory",
			FS:   memfs,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("listening; now run: rsync -av rsync://%s/inmemory", listener.Addr())

	if err := rsyncServer.Serve(context.Background(), listener); err != nil {
		log.Fatal(err)
	}
}
