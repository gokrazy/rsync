// Tool gokr-rsyncd is a read-only rsync daemon sender-only Go implementation of
// rsyncd. rsync daemon is a custom (un-standardized) network protocol, running
// on port 873 by default.
//
// For the corresponding way of operation in the original “tridge” rsync
// (https://github.com/WayneD/rsync), see
// https://manpages.debian.org/bullseye/rsync/rsync.1.en.html#DAEMON_OPTIONS
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gokrazy/rsync/internal/maincmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if _, err := maincmd.Main(ctx, os.Args, os.Stdin, os.Stdout, os.Stderr, nil); err != nil {
		log.Fatal(err)
	}
}
