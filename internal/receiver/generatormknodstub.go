//go:build !linux && !darwin

package receiver

import "io/fs"

func (rt *Transfer) createDevice(*file, fs.FileInfo) error {
	return nil
}
