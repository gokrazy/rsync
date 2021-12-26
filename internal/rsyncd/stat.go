//go:build !linux && !darwin

package rsyncd

import "io/fs"

func uidFromFileInfo(fs.FileInfo) (int32, bool) {
	return 0, false
}

func gidFromFileInfo(fs.FileInfo) (int32, bool) {
	return 0, false
}

func rdevFromFileInfo(fs.FileInfo) (int32, bool) {
	return 0, false
}
