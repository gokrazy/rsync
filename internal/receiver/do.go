package receiver

import (
	"context"

	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/rsyncwire"
	"golang.org/x/sync/errgroup"
)

type Stats struct {
	Read    int64 // total bytes read (from network connection)
	Written int64 // total bytes written (to network connection)
	Size    int64 // total size of files
}

// rsync/main.c:do_recv
func (rt *Transfer) Do(c *rsyncwire.Conn, fileList []*File, noReport bool) (*Stats, error) {
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
