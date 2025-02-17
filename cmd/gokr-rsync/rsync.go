// Tool gokr-rsync is an rsync Go implementation.
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
