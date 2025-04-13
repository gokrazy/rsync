package sender

import (
	"fmt"
	"sort"

	"github.com/gokrazy/rsync/internal/rsyncstats"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

// rsync/main.c:handle_stats
func (st *Transfer) handleStats(crd *rsyncwire.CountingReader, cwr *rsyncwire.CountingWriter, fileList *fileList) error {
	if !st.Opts.Server() || !st.Opts.Sender() {
		return nil
	}

	// send statistics:
	// total bytes read (from network connection)
	if err := st.Conn.WriteInt64(crd.BytesRead); err != nil {
		return err
	}
	// total bytes written (to network connection)
	if err := st.Conn.WriteInt64(cwr.BytesWritten); err != nil {
		return err
	}
	// total size of files
	if err := st.Conn.WriteInt64(fileList.TotalSize); err != nil {
		return err
	}
	return nil
}

// rsync/main.c:client_run am_sender
func (st *Transfer) Do(crd *rsyncwire.CountingReader, cwr *rsyncwire.CountingWriter, modPath string, paths []string, exclusionList *filterRuleList) (*rsyncstats.TransferStats, error) {
	if exclusionList == nil {
		exclusionList = &filterRuleList{}
	}

	// “Update exchange” as per
	// https://github.com/kristapsdz/openrsync/blob/master/rsync.5

	// send file list
	st.Logger.Printf("SendFileList(modPath=%q, paths=%q)", modPath, paths)
	fileList, err := st.SendFileList(modPath, st.Opts, paths, exclusionList)
	if err != nil {
		return nil, err
	}
	defer fileList.Close()

	if st.Opts.Verbose() { // TODO: DebugGTE(FLIST, 3)
		st.Logger.Printf("file list sent")
	}

	// Sort the file list. The client sorts, so we need to sort, too (in the
	// same way!), otherwise our indices do not match what the client will
	// request.
	sort.Slice(fileList.Files, func(i, j int) bool {
		return fileList.Files[i].Wpath < fileList.Files[j].Wpath
	})

	if err := st.SendFiles(fileList); err != nil {
		return nil, err
	}

	if err := st.handleStats(crd, cwr, fileList); err != nil {
		return nil, err
	}

	if st.Opts.Verbose() { // TODO: Debug
		st.Logger.Printf("reading final int32")
	}

	finish, err := st.Conn.ReadInt32()
	if err != nil {
		return nil, err
	}
	if finish != -1 {
		return nil, fmt.Errorf("protocol error: expected final -1, got %d", finish)
	}

	return &rsyncstats.TransferStats{
		Read:    crd.BytesRead,
		Written: cwr.BytesWritten,
		Size:    fileList.TotalSize,
	}, nil
}
