package receiver

import (
	"os"

	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

// TransferOpts is a subset of Opts which is required for implementing a receiver.
type TransferOpts struct {
	Verbose bool
	DryRun  bool
	Server  bool

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
}

type Transfer struct {
	// config
	Logger   log.Logger
	Opts     *TransferOpts
	Dest     string
	DestRoot *os.Root
	Env      *rsyncos.Env

	// state
	Conn     *rsyncwire.Conn
	Seed     int32
	IOErrors int32
	Users    map[int32]mapping
	Groups   map[int32]mapping
}

func (rt *Transfer) listOnly() bool { return rt.Dest == "" }
