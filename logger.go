package rsync

import "github.com/gokrazy/rsync/internal/log"

// Logger is an interface that allows specifying your own logger.
// By default, the Go log package is used, which prints to stderr.
type Logger = log.Logger
