//go:build !linux && !darwin

package receivermaincmd

import "io/fs"

func (rt *recvTransfer) createDevice(*file, fs.FileInfo) error {
	return nil
}
