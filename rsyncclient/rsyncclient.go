package rsyncclient

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/gokrazy/rsync/internal/maincmd"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
)

// Option specifies the client options.
type Option interface {
	applyServer(*Client)
}

type clientOptionFunc func(server *Client)

func (f clientOptionFunc) applyServer(s *Client) {
	f(s)
}

func WithStderr(stderr io.Writer) Option {
	return clientOptionFunc(func(c *Client) {
		c.osenv.Stderr = stderr
	})
}

func WithSender() Option {
	return clientOptionFunc(func(c *Client) {
		c.sender = true
	})
}

func WithNegotiate(negotiate bool) Option {
	return clientOptionFunc(func(c *Client) {
		c.negotiate = negotiate
	})
}

type Client struct {
	osenv     rsyncos.Std
	opts      *rsyncopts.Options
	negotiate bool
	sender    bool
}

func New(args []string, opts ...Option) (*Client, error) {
	c := &Client{
		osenv: rsyncos.Std{
			Stderr: os.Stderr,
		},
		negotiate: true,
	}

	for _, opt := range opts {
		opt.applyServer(c)
	}

	pc, err := rsyncopts.ParseArguments(args)
	if err != nil {
		return nil, err
	}
	c.opts = pc.Options
	if len(pc.RemainingArgs) > 0 {
		return nil, fmt.Errorf("remaining args %q not permitted; specify them in Client.Run()", pc.RemainingArgs)
	}
	if c.sender {
		c.opts.SetSender()
	}

	return c, nil
}

func (c *Client) ServerOptions() []string {
	return c.opts.ServerOptions()
}

func (c *Client) ServerCommandOptions(path string, paths ...string) []string {
	return c.opts.CommandOptions(path, paths...)
}

func (c *Client) Run(ctx context.Context, conn io.ReadWriter, paths []string) error {
	stats, err := maincmd.ClientRun(c.osenv, c.opts, conn, paths, c.negotiate)
	if err != nil {
		return err
	}
	log.Printf("stats: %+v", stats) // TODO: remove
	return nil
}
