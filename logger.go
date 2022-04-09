package rsync

import "github.com/gokrazy/rsync/internal/log"

// Logger logs messages.
type Logger = log.Logger

// SetLogger overrides the default logger to use in rsync.
// This should be call from the very beggining of the program.
func SetLogger(logger Logger) {
	log.SetLogger(logger)
}
