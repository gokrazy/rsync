//go:build !linux && !darwin

package receivermaincmd

import "io/fs"

func (rt *recvTransfer) setUid(_ *file, _ string, st fs.FileInfo) (fs.FileInfo, error) {
	return st, nil
}
