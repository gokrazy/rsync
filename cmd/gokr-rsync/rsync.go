// Tool gokr-rsync is an rsync receiver Go implementation.
package main

import (
	"log"
	"os"

	"github.com/gokrazy/rsync/internal/maincmd"
)

func main() {
	if _, err := maincmd.ClientMain(os.Args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		log.Fatal(err)
	}
}
