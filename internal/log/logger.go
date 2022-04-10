// Package log defines the logger interface used in rsync library.
package log

import "log"

// Logger is an interface that allows specifying your own logger.
// By default, the Go log package is used, which prints to stderr.
type Logger interface {
	// Printf logs message to the underlying log output. Arguments are handled in the manner of fmt.Printf.
	Printf(msg string, a ...interface{})
}

// instance is the global instance of the logger.
// Default logger is log.Logger.
var instance Logger = log.Default()

// Printf logs message to the default logger.
func Printf(msg string, a ...interface{}) {
	instance.Printf(msg, a...)
}

// SetLogger overrides the default logger to use in rsync.
// This should be call from the very beggining of the program.
func SetLogger(logger Logger) {
	instance = logger
}

// Default returns the global logger instance.
func Default() Logger {
	return instance
}
