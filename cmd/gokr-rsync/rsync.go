// Tool gokr-rsync is an rsync receiver Go implementation.
package main

import (
	"log"
	"os"

	maincmd "github.com/gokrazy/rsync/internal/daemonmaincmd"
)

func main() {
	if _, err := maincmd.ClientMain(os.Args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		log.Fatal(err)
	}
}
