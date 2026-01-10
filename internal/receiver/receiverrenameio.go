//go:build linux || darwin

package receiver

import (
	"os"

	"github.com/google/renameio/v2"
)

func newPendingFile(root *os.Root, fn string) (*renameio.PendingFile, error) {
	return renameio.NewPendingFile(fn, renameio.WithRoot(root))
}
