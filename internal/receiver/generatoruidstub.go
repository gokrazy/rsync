//go:build !linux && !darwin

package receiver

import "io/fs"

func (rt *Transfer) setUid(_ *file, _ string, st fs.FileInfo) (fs.FileInfo, error) {
	return st, nil
}
