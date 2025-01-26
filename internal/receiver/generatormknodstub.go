//go:build !linux && !darwin

package receiver

import "io/fs"

func (rt *Transfer) createDevice(*File, fs.FileInfo) error {
	return nil
}
