package rsyncostest

import (
	"testing"

	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/testlogger"
)

func New(t *testing.T) *rsyncos.Env {
	return &rsyncos.Env{
		// Logs go to stderr, so wire that up to a testlogger.
		Stderr: testlogger.New(t),
	}
}
