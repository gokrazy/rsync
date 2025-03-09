package receiver

import (
	"io"
	"os/user"
	"strconv"
)

type mapping struct {
	Name    string
	LocalId int32
}

func (rt *Transfer) recvIdMapping1(localId func(id int32, name string) int32) (map[int32]mapping, error) {
	idMapping := make(map[int32]mapping)
	for {
		id, err := rt.Conn.ReadInt32()
		if err != nil {
			return nil, err
		}
		if id == 0 {
			break
		}
		length, err := rt.Conn.ReadByte()
		if err != nil {
			return nil, err
		}
		name := make([]byte, length)
		if _, err := io.ReadFull(rt.Conn.Reader, name); err != nil {
			return nil, err
		}
		idMapping[id] = mapping{
			Name:    string(name),
			LocalId: localId(id, string(name)),
		}
	}
	return idMapping, nil
}

// rsync/uidlist.c:recv_id_list
func (rt *Transfer) RecvIdList() (users map[int32]mapping, groups map[int32]mapping, _ error) {
	if rt.Opts.PreserveUid {
		var err error
		users, err = rt.recvIdMapping1(func(remoteUid int32, remoteUsername string) int32 {
			u, err := user.Lookup(remoteUsername)
			if err != nil {
				return remoteUid
			}
			uid, err := strconv.ParseInt(u.Uid, 0, 32)
			if err != nil {
				return remoteUid
			}
			return int32(uid)
		})
		if err != nil {
			return nil, nil, err
		}
		for remoteUid, mapping := range users {
			rt.Logger.Printf("remote uid %d(%s) maps to local uid %d", remoteUid, mapping.Name, mapping.LocalId)
		}
	}

	if rt.Opts.PreserveGid {
		var err error
		groups, err = rt.recvIdMapping1(func(remoteGid int32, remoteGroupname string) int32 {
			g, err := user.LookupGroup(remoteGroupname)
			if err != nil {
				return remoteGid
			}
			gid, err := strconv.ParseInt(g.Gid, 0, 32)
			if err != nil {
				return remoteGid
			}
			return int32(gid)
		})
		if err != nil {
			return nil, nil, err
		}
		for remoteGid, mapping := range groups {
			rt.Logger.Printf("remote gid %d(%s) maps to local gid %d", remoteGid, mapping.Name, mapping.LocalId)
		}
	}

	return users, groups, nil
}
