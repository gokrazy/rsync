package receivermaincmd

import (
	"io"

	maincmd "github.com/gokrazy/rsync/internal/daemonmaincmd"
	"github.com/gokrazy/rsync/internal/rsyncstats"
)

func ClientMain(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (*rsyncstats.TransferStats, error) {
	return maincmd.ClientMain(args, stdin, stdout, stderr)
}
