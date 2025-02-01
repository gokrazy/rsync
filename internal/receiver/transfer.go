package receiver

import (
	"io"

	"github.com/gokrazy/rsync/internal/rsyncwire"
)

type Osenv struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// TransferOpts is a subset of Opts which is required for implementing a receiver.
type TransferOpts struct {
	Verbose bool
	DryRun  bool

	DeleteMode        bool
	PreserveGid       bool
	PreserveUid       bool
	PreserveLinks     bool
	PreservePerms     bool
	PreserveDevices   bool
	PreserveSpecials  bool
	PreserveTimes     bool
	PreserveHardlinks bool
}

type Transfer struct {
	// config
	// Opts *Opts
	Opts *TransferOpts
	Dest string
	Env  Osenv

	// state
	Conn     *rsyncwire.Conn
	Seed     int32
	IOErrors int32
}

func (rt *Transfer) listOnly() bool { return rt.Dest == "" }
