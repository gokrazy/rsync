// Tool gokr-rsyncd is an old name for gokr-rsync.
//
// Please update your setup to install/use gokr-rsync directly instead.
//
// This program will be removed in a future release.
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
