package flist_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/rsyncd"
)

// rsynctest.go:282: length 280063 exceeds max message size (262144)
func TestLargeFileList(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	source := filepath.Join(tmp, "source")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	for i := range 5000 {
		fn := fmt.Sprintf("file_with_long_name_number_%04d", i)
		if err := os.WriteFile(filepath.Join(source, fn), []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(tmp, "dest")

	// start a server to sync from
	srv := rsynctest.NewInMemory(t, rsyncd.Module{
		Name: "interop",
		Path: source,
	})
	srv.RunClient(t, []string{"-aH"}, []string{dest})
}
