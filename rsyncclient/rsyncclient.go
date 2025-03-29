// Package rsyncclient implements an rsync client (only), but note that
// gokrazy/rsync contains a native Go rsync implementation that supports sending
// and receiving files as client or server, compatible with the original tridge
// rsync (from the samba project) or openrsync (used on OpenBSD and macOS 15+).
package rsyncclient

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gokrazy/rsync/internal/maincmd"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncstats"
)

// Option specifies the client options.
type Option interface {
	applyServer(*Client)
}

type clientOptionFunc func(server *Client)

func (f clientOptionFunc) applyServer(s *Client) {
	f(s)
}

// WithStderr makes the [Client] write to the specified stderr instead of
// [os.Stderr].
func WithStderr(stderr io.Writer) Option {
	return clientOptionFunc(func(c *Client) {
		c.osenv.Stderr = stderr
	})
}

// WithSender enables sender mode (receiver by default).
func WithSender() Option {
	return clientOptionFunc(func(c *Client) {
		c.sender = true
	})
}

// WithoutNegotiate disables protocol version negotiation (enabled by default).
func WithoutNegotiate() Option {
	return clientOptionFunc(func(c *Client) {
		c.negotiate = false
	})
}

func DontRestrict() Option {
	return clientOptionFunc(func(c *Client) {
		c.osenv.DontRestrict = true
	})
}

type Client struct {
	osenv     rsyncos.Env
	opts      *rsyncopts.Options
	negotiate bool
	sender    bool
}

// New creates a new [Client]. You can call [Client.Run] one or more times with
// the same [Client].
func New(args []string, opts ...Option) (*Client, error) {
	c := &Client{
		osenv: rsyncos.Env{
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

// ServerCommandOptions returns the options that rsync would use to spawn the
// server process.
func (c *Client) ServerCommandOptions(path string, paths ...string) []string {
	return c.opts.CommandOptions(path, paths...)
}

// Result contains information about a transfer.
type Result struct {
	Stats *rsyncstats.TransferStats
}

// Run starts one run of the rsync protocol (not the rsync daemon protocol), see
// also https://michael.stapelberg.ch/posts/2022-07-02-rsync-how-does-it-work/.
//
// If you just want to transfer some data from an already running rsync server
// or remote system, use the [github.com/gokrazy/rsync/rsynccmd] package
// instead, which will take care of starting rsync as a subprocess (locally or
// remotely), or of connecting to an rsync daemon via TCP.
//
// The Run method operates on any kind of connection (using the [io.ReadWriter]
// interface) and is meant to be used when you need more control over the
// setup. For example, maybe you want to set up some custom tunneling to an
// rsync process running deep in some remote cloud infrastructure.
//
// Or maybe you want to connect an rsync client and server to each other via a
// custom RPC protocol. In that case, you will need to transport the
// [Client.ServerCommandOptions] to the server and then arrange for two
// [io.ReadWriter] connections between client and server.
func (c *Client) Run(ctx context.Context, conn io.ReadWriter, paths []string) (*Result, error) {
	stats, err := maincmd.ClientRun(c.osenv, c.opts, conn, paths, c.negotiate)
	if err != nil {
		return nil, err
	}
	return &Result{Stats: stats}, nil
}
