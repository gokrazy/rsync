// Tool gokr-rsyncd is a read-only rsync daemon sender-only Go implementation of
// rsyncd. rsync daemon is a custom (un-standardized) network protocol, running
// on port 873 by default.
//
// For the corresponding way of operation in the original “tridge” rsync
// (https://github.com/WayneD/rsync), see
// https://manpages.debian.org/bullseye/rsync/rsync.1.en.html#DAEMON_OPTIONS
package main

import (
	"log"

	"github.com/gokrazy/rsync/internal/maincmd"
)

func main() {
	if err := maincmd.Main(); err != nil {
		log.Fatal(err)
	}
}
