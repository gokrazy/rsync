// Package log defines the logger interface used in rsync library.
package log

import (
	"io"
	"log"
)

// Logger is an interface that allows specifying your own logger.
// By default, the Go log package is used, which prints to stderr.
type Logger interface {
	// Printf logs message to the underlying log output. Arguments are handled in the manner of fmt.Printf.
	Printf(msg string, a ...any)

	Output(calldepth int, s string) error
}

const logFlags = log.LstdFlags | log.Lshortfile

func New(out io.Writer) Logger {
	return log.New(out, "", logFlags)
}
