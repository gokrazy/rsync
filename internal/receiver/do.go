package receiver

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/rsyncwire"
	"golang.org/x/sync/errgroup"
)

type Stats struct {
	Read    int64 // total bytes read (from network connection)
	Written int64 // total bytes written (to network connection)
	Size    int64 // total size of files
}

func isTopDir(f *File) bool {
	// TODO: once we check the f.Flags:
	// if !f.FileMode().IsDir() {
	//    // non-directories can get the top_dir flag set,
	//    // but it must be ignored (only for protocol reasons).
	//   return false
	// }
	// return (f.Flags & TOP_DIR) != 0
	return f.Name == "."
}

func (rt *Transfer) deleteFiles(fileList []*File) error {
	if rt.IOErrors > 0 {
		log.Printf("IO error encountered, skipping file deletion")
		return nil
	}

	for _, f := range fileList {
		if !isTopDir(f) {
			continue
		}
		log.Printf("deleting in %s", f.Name)
		root := filepath.Clean(rt.Dest)
		strip := root + "/"
		// Other rsync implementations generate a local file list and compare it
		// with the remote file list, we re-implement the path→name mapping part
		// of file list generation here. We could change it for consistency.
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			name := strings.TrimPrefix(path, strip)
			if name == root {
				name = "."
			}
			if findInFileList(fileList, name) {
				return nil
			}
			if rt.Opts.Verbose {
				log.Printf("  deleting %s", name)
			}
			if rt.Opts.DryRun {
				return nil
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			if os.IsNotExist(err) {
				return nil // destination does not exist, nothing to do
			}
			return err
		}
	}
	return nil
}

// rsync/main.c:do_recv
func (rt *Transfer) Do(c *rsyncwire.Conn, fileList []*File, noReport bool) (*Stats, error) {
	if rt.Opts.DeleteMode {
		if err := rt.deleteFiles(fileList); err != nil {
			return nil, err
		}
	}

	ctx := context.Background()
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return rt.GenerateFiles(fileList)
	})
	eg.Go(func() error {
		// Ensure we don’t block on the receiver when the generator returns an
		// error.
		errChan := make(chan error)
		go func() {
			errChan <- rt.RecvFiles(fileList)
		}()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		}
	})
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	var stats *Stats
	if !noReport {
		var err error
		stats, err = report(c)
		if err != nil {
			return nil, err
		}
	}

	// send final goodbye message
	if err := c.WriteInt32(-1); err != nil {
		return nil, err
	}

	return stats, nil
}

// rsync/main.c:report
func report(c *rsyncwire.Conn) (*Stats, error) {
	// read statistics:
	// total bytes read (from network connection)
	read, err := c.ReadInt64()
	if err != nil {
		return nil, err
	}
	// total bytes written (to network connection)
	written, err := c.ReadInt64()
	if err != nil {
		return nil, err
	}
	// total size of files
	size, err := c.ReadInt64()
	if err != nil {
		return nil, err
	}
	log.Printf("server sent stats: read=%d, written=%d, size=%d", read, written, size)

	return &Stats{
		Read:    read,
		Written: written,
		Size:    size,
	}, nil
}
