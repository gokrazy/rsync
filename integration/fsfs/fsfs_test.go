package fsfs_test

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/rsyncd"
	"github.com/google/go-cmp/cmp"
)

func TestMain(m *testing.M) {
	if err := rsynctest.CommandMain(m); err != nil {
		log.Fatal(err)
	}
}

func TestMapFS(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	dest := filepath.Join(tmp, "dest")

	memfs := fstest.MapFS{
		"hello.txt": &fstest.MapFile{
			Data:    []byte("world"),
			Mode:    0o644,
			ModTime: rsynctest.GosPublicRelease,
		},
		"bye.txt": &fstest.MapFile{
			Data:    []byte("moon"),
			Mode:    0o644,
			ModTime: rsynctest.GosPublicRelease,
		},
	}

	srv := rsynctest.NewInMemory(t, rsyncd.Module{
		Name: "memfs",
		FS:   memfs,
	})
	args := []string{"-av"}
	srv.RunClient(t, args, []string{dest + "/"})

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("hello.txt: unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
	{
		want := []byte("moon")
		got, err := os.ReadFile(filepath.Join(dest, "bye.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("world.txt: unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}

	// Restore write permission so that t.TempDir() cleanup succeeds
	if err := os.Chmod(dest, 0755); err != nil {
		t.Fatal(err)
	}
}

func TestMapFSErrors(t *testing.T) {
	t.Parallel()

	memfs := fstest.MapFS{
		"hello.txt": &fstest.MapFile{
			Data:    []byte("world"),
			Mode:    0o644,
			ModTime: rsynctest.GosPublicRelease,
		},
	}
	_, err := rsyncd.NewServer([]rsyncd.Module{
		{
			Name:     "test",
			FS:       memfs,
			Writable: true,
		},
	})
	if err == nil {
		t.Fatal("expected error for writable FS module, got nil")
	}

	_, err = rsyncd.NewServer([]rsyncd.Module{
		{
			Name: "test",
			Path: "/",
			FS:   memfs,
		},
	})
	if err == nil {
		t.Fatal("expected error for module with Path and FS, got nil")
	}
}

func TestMapFSLargeFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	dest := filepath.Join(tmp, "dest")
	destLarge := filepath.Join(dest, "large.bin")

	headPattern := []byte{0x11}
	bodyPattern := []byte{0xbb}
	endPattern := []byte{0xee}
	large := rsynctest.ConstructLargeDataFile(headPattern, bodyPattern, endPattern)

	memfs := fstest.MapFS{
		"hello.txt": &fstest.MapFile{
			Data:    []byte("world"),
			Mode:    0o644,
			ModTime: rsynctest.GosPublicRelease,
		},
		"large.bin": &fstest.MapFile{
			Data:    large,
			Mode:    0o644,
			ModTime: rsynctest.GosPublicRelease,
		},
	}

	srv := rsynctest.NewInMemory(t, rsyncd.Module{
		Name: "memfs",
		FS:   memfs,
	})
	args := []string{"-av"}
	firstStats := srv.RunClient(t, args, []string{dest})
	t.Logf("firstStats: %+v", firstStats)

	{
		want := []byte("world")
		got, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("hello.txt: unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}

	if err := rsynctest.DataFileMatches(destLarge, headPattern, bodyPattern, endPattern); err != nil {
		t.Fatal(err)
	}

	// Change the middle of the large data file:
	bodyPattern = []byte{0x66}
	// modify the large data file in memory
	large = rsynctest.ConstructLargeDataFile(headPattern, bodyPattern, endPattern)
	memfs["large.bin"].Data = large

	incrementalStats := srv.RunClient(t, args, []string{dest})
	t.Logf("incrementalStats: %+v", incrementalStats)
	if got, want := incrementalStats.Written, int64(2*1024*1024); got >= want {
		t.Fatalf("rsync unexpectedly transferred more data than needed: got %d, want < %d", got, want)
	}

	// Restore write permission so that t.TempDir() cleanup succeeds
	if err := os.Chmod(dest, 0755); err != nil {
		t.Fatal(err)
	}
}
