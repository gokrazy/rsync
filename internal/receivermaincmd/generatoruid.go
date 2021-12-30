//go:build linux || darwin

package receivermaincmd

import (
	"io/fs"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

var amRoot = os.Getuid() == 0

var inGroup = func() map[uint32]bool {
	m := make(map[uint32]bool)
	u, err := user.Current()
	if err != nil {
		return m
	}
	gids, err := u.GroupIds()
	if err != nil {
		return m
	}
	for _, gidString := range gids {
		gid64, err := strconv.ParseInt(gidString, 0, 64)
		if err != nil {
			return m
		}
		m[uint32(gid64)] = true
	}
	return m
}()

func (rt *recvTransfer) setUid(f *file, local string, st fs.FileInfo) (fs.FileInfo, error) {
	stt := st.Sys().(*syscall.Stat_t)

	changeUid := rt.opts.PreserveUid &&
		amRoot &&
		stt.Uid != uint32(f.Uid)

	changeGid := rt.opts.PreserveGid &&
		(amRoot || inGroup[uint32(f.Gid)]) &&
		stt.Gid != uint32(f.Gid)

	if !changeUid && !changeGid {
		return st, nil
	}

	uid := stt.Uid
	if changeUid {
		uid = uint32(f.Uid)
	}
	gid := stt.Gid
	if changeGid {
		gid = uint32(f.Gid)
	}
	if err := os.Lchown(local, int(uid), int(gid)); err != nil {
		return nil, err
	}
	return os.Lstat(local)
}
