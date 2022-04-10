package rsyncd

import (
	"encoding/binary"
	"io"
	"os"
	"sort"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/rsyncchecksum"
	"github.com/gokrazy/rsync/internal/rsynccommon"
	"github.com/mmcloughlin/md4"
	"golang.org/x/sync/errgroup"
)

// rsync/sender.c:send_files()
func (st *sendTransfer) sendFiles(fileList *fileList) error {
	phase := 0
	for {
		// receive data about receiver’s copy of the file list contents (not
		// ordered)
		// see (*rsync.Receiver).Generator()
		fileIndex, err := st.conn.ReadInt32()
		if err != nil {
			return err
		}
		if fileIndex == -1 {
			if phase == 0 {
				phase++
				// acknowledge phase change by sending -1
				if err := st.conn.WriteInt32(-1); err != nil {
					return err
				}
				continue
			}
			break
		}

		if st.opts.DryRun {
			if err := st.conn.WriteInt32(fileIndex); err != nil {
				return err
			}
			continue
		}

		head, err := st.receiveSums()
		if err != nil {
			return err
		}

		// The following quotes are citations from
		// https://www.samba.org/~tridge/phd_thesis.pdf, section 3.2.6 The
		// signature search algorithm (PDF page 64).

		// rsync/match.c:build_hash_table
		targets := make([]target, len(head.Sums))
		tagTable := make(map[uint16]int) // TODO: or int32 more specifically?
		{
			// “The first step in the algorithm is to sort the received
			// signatures by a 16 bit hash of the fast signature.”
			for idx, sum := range head.Sums {
				targets[idx] = target{
					index: int32(idx),
					tag:   rsyncchecksum.Tag(sum.Sum1),
				}
			}
			sort.Slice(targets, func(i, j int) bool {
				return targets[i].tag < targets[j].tag
			})

			// “A 16 bit index table is then formed which takes a 16 bit hash
			// value and gives an index into the sorted signature table which
			// points to the first entry in the table which has a matching
			// hash.”
			for idx := range head.Sums {
				tagTable[targets[idx].tag] = idx
			}
		}

		st.lastMatch = 0
		if len(head.Sums) == 0 {
			// fast path: send the whole file
			err = st.sendFile(fileIndex, fileList.files[fileIndex])
		} else {
			err = st.hashSearch(targets, tagTable, head, fileIndex, fileList.files[fileIndex])
		}
		if err != nil {
			if _, ok := err.(*os.PathError); ok {
				// OpenFile() failed. Log the error (server side only) and
				// proceed. Only starting with protocol 30, an I/O error flag is
				// sent after the file transfer phase.
				if os.IsNotExist(err) {
					st.logger.Printf("file has vanished: %s", fileList.files[fileIndex].path)
				} else {
					st.logger.Printf("sendFiles: %v", err)
				}
				continue
			} else {
				return err
			}
		}
	}

	// phase done
	if err := st.conn.WriteInt32(-1); err != nil {
		return err
	}

	return nil
}

// rsync/sender.c:receive_sums()
func (st *sendTransfer) receiveSums() (rsync.SumHead, error) {
	var head rsync.SumHead
	if err := head.ReadFrom(st.conn); err != nil {
		return head, err
	}
	var offset int64
	head.Sums = make([]rsync.SumBuf, int(head.ChecksumCount))
	for i := int32(0); i < head.ChecksumCount; i++ {
		shortChecksum, err := st.conn.ReadInt32()
		if err != nil {
			return head, err
		}
		sb := rsync.SumBuf{
			Index:  i,
			Offset: offset,
			Sum1:   uint32(shortChecksum),
		}
		if i == head.ChecksumCount-1 && head.RemainderLength != 0 {
			sb.Len = int64(head.RemainderLength)
		} else {
			sb.Len = int64(head.BlockLength)
		}
		offset += sb.Len
		n, err := io.ReadFull(st.conn.Reader, sb.Sum2[:head.ChecksumLength])
		if err != nil {
			return head, err
		}
		_ = n
		// st.logger.Printf("chunk[%d] len=%d offset=%.0f sum1=%08x, sum2=%x",
		// 	i, sb.len, float64(sb.offset), sb.sum1, sb.sum2[:n])
		head.Sums[i] = sb
	}
	return head, nil
}

func (st *sendTransfer) sendFile(fileIndex int32, fl file) error {
	// rsync/rsync.h defines chunkSize as 32 * 1024, but increasing it to 256K
	// increases throughput with “tridge” rsync as client by 50 Mbit/s.
	const chunkSize = 256 * 1024

	f, err := os.Open(fl.path)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	if err := st.conn.WriteInt32(fileIndex); err != nil {
		return err
	}

	sh := rsynccommon.SumSizesSqroot(fi.Size())
	// st.logger.Printf("sh = %+v", sh)
	if err := sh.WriteTo(st.conn); err != nil {
		return err
	}

	h := md4.New()
	binary.Write(h, binary.LittleEndian, st.seed)

	// Calculate the md4 hash in a goroutine.
	//
	// This allows an rsync connection to benefit from more than 1 core!
	//
	// We calculate the hash by opening the same file again and reading
	// independently. This keeps the hot loop below focused on shoveling data
	// into the network socket as quickly as possible.
	var eg errgroup.Group
	eg.Go(func() error {
		f, err := os.Open(fl.path)
		if err != nil {
			return err
		}
		defer f.Close()
		var buf [chunkSize]byte
		if _, err := io.CopyBuffer(h, f, buf[:]); err != nil {
			return err
		}
		return nil
	})

	buf := make([]byte, chunkSize)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		chunk := buf[:n]
		// chunk size (“rawtok” variable in openrsync)
		if err := st.conn.WriteInt32(int32(len(chunk))); err != nil {
			return err
		}
		if _, err := st.conn.Writer.Write(chunk); err != nil {
			return err
		}
	}
	// transfer finished:
	if err := st.conn.WriteInt32(0); err != nil {
		return err
	}

	// whole file long checksum (16 bytes)
	if err := eg.Wait(); err != nil {
		return err
	}
	sum := h.Sum(nil)
	// st.logger.Printf("sum: %x (len = %d)", sum, len(sum))
	if _, err := st.conn.Writer.Write(sum); err != nil {
		return err
	}
	return nil
}
