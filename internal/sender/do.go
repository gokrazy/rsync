package sender

import (
	"fmt"
	"sort"

	"github.com/gokrazy/rsync/internal/rsyncwire"
)

type Stats struct {
	Read    int64 // total bytes read (from network connection)
	Written int64 // total bytes written (to network connection)
	Size    int64 // total size of files
}

// rsync/main.c:client_run am_sender
func (st *Transfer) Do(crd *rsyncwire.CountingReader, cwr *rsyncwire.CountingWriter, modName, modPath string, paths []string) (*Stats, error) {
	// receive the exclusion list (openrsync’s is always empty)
	exclusionList, err := RecvFilterList(st.Conn)
	if err != nil {
		return nil, err
	}
	st.Logger.Printf("exclusion list read (entries: %d)", len(exclusionList.Filters))

	// “Update exchange” as per
	// https://github.com/kristapsdz/openrsync/blob/master/rsync.5

	// send file list
	fileList, err := st.SendFileList(modName, modPath, st.Opts, paths, exclusionList)
	if err != nil {
		return nil, err
	}

	st.Logger.Printf("file list sent")

	// Sort the file list. The client sorts, so we need to sort, too (in the
	// same way!), otherwise our indices do not match what the client will
	// request.
	sort.Slice(fileList.Files, func(i, j int) bool {
		return fileList.Files[i].Wpath < fileList.Files[j].Wpath
	})

	if err := st.SendFiles(fileList); err != nil {
		return nil, err
	}

	// send statistics:
	// total bytes read (from network connection)
	if err := st.Conn.WriteInt64(crd.BytesRead); err != nil {
		return nil, err
	}
	// total bytes written (to network connection)
	if err := st.Conn.WriteInt64(cwr.BytesWritten); err != nil {
		return nil, err
	}
	// total size of files
	if err := st.Conn.WriteInt64(fileList.TotalSize); err != nil {
		return nil, err
	}

	st.Logger.Printf("reading final int32")

	finish, err := st.Conn.ReadInt32()
	if err != nil {
		return nil, err
	}
	if finish != -1 {
		return nil, fmt.Errorf("protocol error: expected final -1, got %d", finish)
	}

	return &Stats{
		Read:    crd.BytesRead,
		Written: cwr.BytesWritten,
		Size:    fileList.TotalSize,
	}, nil
}
