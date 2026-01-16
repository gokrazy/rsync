package receiver

import (
	"os"

	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/progress"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

// TransferOpts is a subset of Opts which is required for implementing a receiver.
type TransferOpts struct {
	Verbose  bool
	DryRun   bool
	Server   bool
	Progress bool

	DeleteMode        bool
	PreserveGid       bool
	PreserveUid       bool
	PreserveLinks     bool
	PreservePerms     bool
	PreserveDevices   bool
	PreserveSpecials  bool
	PreserveTimes     bool
	PreserveHardlinks bool
	IgnoreTimes       bool
	AlwaysChecksum    bool

	InfoGTE  func(rsyncopts.InfoLevel, uint16) bool
	DebugGTE func(rsyncopts.DebugLevel, uint16) bool
}

type Transfer struct {
	// config
	Logger   log.Logger
	Opts     *TransferOpts
	Dest     string
	DestRoot *os.Root
	Env      *rsyncos.Env
	Progress progress.Printer

	// state
	Conn            *rsyncwire.Conn
	Seed            int32
	IOErrors        int32
	Users           map[int32]mapping
	Groups          map[int32]mapping
	retouchDirPerms bool
}

func (rt *Transfer) listOnly() bool { return rt.Dest == "" }
