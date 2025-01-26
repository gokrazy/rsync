//go:build linux || darwin

package receiver

import "github.com/google/renameio/v2"

func newPendingFile(fn string) (*renameio.PendingFile, error) {
	return renameio.NewPendingFile(fn)
}
