//go:build linux || darwin

package rsyncd

import (
	"io/fs"
	"syscall"
)

func uidFromFileInfo(info fs.FileInfo) (int32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int32(st.Uid), true
}

func gidFromFileInfo(info fs.FileInfo) (int32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int32(st.Gid), true
}

func rdevFromFileInfo(info fs.FileInfo) (int32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int32(st.Rdev), true
}
