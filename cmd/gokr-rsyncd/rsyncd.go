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

	"github.com/gokrazy/rsync/rsynccmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := rsynccmd.Command(os.Args[0], os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if _, err := cmd.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
