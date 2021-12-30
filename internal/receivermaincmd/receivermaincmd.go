package receivermaincmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/gokrazy/rsync/internal/rsyncwire"
	"golang.org/x/sync/errgroup"
)

type osenv struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

type recvTransfer struct {
	// config
	opts *Opts
	dest string
	env  osenv

	// state
	conn *rsyncwire.Conn
	seed int32
}

func (rt *recvTransfer) listOnly() bool { return rt.dest == "" }

type Stats struct {
	Read    int64 // total bytes read (from network connection)
	Written int64 // total bytes written (to network connection)
	Size    int64 // total size of files
}

// rsync/main.c:start_client
func rsyncMain(osenv osenv, opts *Opts, sources []string, dest string) (*Stats, error) {
	log.Printf("dest: %q, sources: %q", dest, sources)
	log.Printf("opts: %+v", opts)
	for _, src := range sources {
		log.Printf("processing src=%s", src)
		if strings.HasPrefix(src, "rsync://") {
			// rsync://[USER@]HOST[:PORT]/SRC
			stats, err := socketClient(osenv, opts, src, dest)
			if err != nil {
				return nil, err
			}
			return stats, nil
		} else {
			// [USER@]HOST:SRC (remote shell)
			// [USER@]HOST::SRC (rsync daemon)
		}
	}
	return nil, nil
}

// rsync/main.c:client_run
func clientRun(osenv osenv, opts *Opts, conn io.ReadWriter, dest string) (*Stats, error) {
	c := &rsyncwire.Conn{
		Reader: conn,
		Writer: conn,
	}

	seed, err := c.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("reading seed: %v", err)
	}

	mrd := &rsyncwire.MultiplexReader{
		Reader: conn,
	}
	// TODO: rearchitect such that our buffer can be smaller than the largest
	// rsync message size
	rd := bufio.NewReaderSize(mrd, 256*1024)
	c.Reader = rd

	rt := &recvTransfer{
		opts: opts,
		dest: dest,
		env:  osenv,
		conn: c,
		seed: seed,
	}

	// TODO: implement support for exclusion, send exclusion list here
	const exclusionListEnd = 0
	if err := c.WriteInt32(exclusionListEnd); err != nil {
		return nil, err
	}

	log.Printf("exclusion list sent")

	// receive file list
	log.Printf("receiving file list")
	fileList, err := rt.receiveFileList()
	if err != nil {
		return nil, err
	}
	log.Printf("received %d names", len(fileList))

	sortFileList(fileList)

	// receive the uid/gid list
	users, groups, err := rt.recvIdList()
	if err != nil {
		return nil, err
	}
	_ = users
	_ = groups

	// read the i/o error flag
	ioErrors, err := c.ReadInt32()
	if err != nil {
		return nil, err
	}
	log.Printf("ioErrors: %v", ioErrors)

	ctx := context.Background()
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return rt.generateFiles(fileList)
	})
	eg.Go(func() error {
		// Ensure we don’t block on the receiver when the generator returns an
		// error.
		errChan := make(chan error)
		go func() {
			errChan <- rt.recvFiles(fileList)
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

	// send final goodbye message
	if err := c.WriteInt32(-1); err != nil {
		return nil, err
	}

	return &Stats{
		Read:    read,
		Written: written,
		Size:    size,
	}, nil
}

// rsync/token.c:recvToken
func (rt *recvTransfer) recvToken() (token int32, data []byte, _ error) {
	var err error
	token, err = rt.conn.ReadInt32()
	if err != nil {
		return 0, nil, err
	}
	if token <= 0 {
		return token, nil, nil
	}
	data = make([]byte, int(token))
	if _, err := io.ReadFull(rt.conn.Reader, data); err != nil {
		return 0, nil, err
	}
	return token, data, nil
}

func Main(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (*Stats, error) {
	osenv := osenv{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
	opts, opt := NewGetOpt()
	remaining, err := opt.Parse(args[1:])
	if opt.Called("help") {
		return nil, errors.New(opt.Help())
	}
	if err != nil {
		return nil, err
	}

	if opts.Archive {
		// --archive is -rlptgoD
		opts.Recurse = true       // -r
		opts.PreserveLinks = true // -l
		opts.PreservePerms = true // -p
		opts.PreserveTimes = true // -t
		opts.PreserveGid = true   // -g
		opts.PreserveUid = true   // -o
		opts.D = true             // -D
	}

	if opts.D {
		opts.PreserveDevices = true
		opts.PreserveSpecials = true
	}

	if len(remaining) == 0 {
		return nil, errors.New(opt.Help())
	}
	if len(remaining) == 1 {
		// Usages with just one SRC arg and no DEST arg list the source files
		// instead of copying.
		dest := ""
		sources := remaining
		return rsyncMain(osenv, opts, sources, dest)
	}
	dest := remaining[len(remaining)-1]
	sources := remaining[:len(remaining)-1]
	return rsyncMain(osenv, opts, sources, dest)
}
