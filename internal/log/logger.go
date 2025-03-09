// Package log defines the logger interface used in rsync library.
package log

import (
	"fmt"
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

// instance is the global instance of the logger.
// Default logger is log.Logger.
var instance = func() Logger {
	log.SetFlags(logFlags)
	return log.Default()
}()

// Printf logs message to the default logger.
func Printf(msg string, a ...any) {
	instance.Output(2, fmt.Sprintf(msg, a...))
}

// SetLogger overrides the default logger to use in rsync.
// This should be call from the very beggining of the program.
func SetLogger(logger Logger) {
	instance = logger
}

func New(out io.Writer) Logger {
	return log.New(out, "", logFlags)
}
