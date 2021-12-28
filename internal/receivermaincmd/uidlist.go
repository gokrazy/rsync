package receivermaincmd

import (
	"io"
	"log"
)

type mapping struct {
	Name    string
	LocalId int32
}

func (rt *recvTransfer) recvIdMapping1(localId func(id int32, name string) int32) (map[int32]mapping, error) {
	idMapping := make(map[int32]mapping)
	for {
		id, err := rt.conn.ReadInt32()
		if err != nil {
			return nil, err
		}
		if id == 0 {
			break
		}
		length, err := rt.conn.ReadByte()
		if err != nil {
			return nil, err
		}
		name := make([]byte, length)
		if _, err := io.ReadFull(rt.conn.Reader, name); err != nil {
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
func (rt *recvTransfer) recvIdList() (users map[int32]mapping, groups map[int32]mapping, _ error) {
	var err error
	users, err = rt.recvIdMapping1(func(remoteUid int32, remoteUsername string) int32 {
		// TODO: look up local uid by username
		return remoteUid
	})
	if err != nil {
		return nil, nil, err
	}
	for remoteUid, mapping := range users {
		log.Printf("remote uid %d(%s) maps to local uid %d", remoteUid, mapping.Name, mapping.LocalId)
	}
	groups, err = rt.recvIdMapping1(func(remoteGid int32, remoteGroupname string) int32 {
		// TODO: look up local gid by groupname
		return remoteGid
	})
	if err != nil {
		return nil, nil, err
	}
	for remoteGid, mapping := range groups {
		log.Printf("remote gid %d(%s) maps to local gid %d", remoteGid, mapping.Name, mapping.LocalId)
	}
	return users, groups, nil
}
