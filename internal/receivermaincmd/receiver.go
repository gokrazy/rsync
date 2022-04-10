package receivermaincmd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/log"
	"github.com/mmcloughlin/md4"
)

// rsync/receiver.c:recv_files
func (rt *recvTransfer) recvFiles(fileList []*file) error {
	phase := 0
	for {
		idx, err := rt.conn.ReadInt32()
		if err != nil {
			return err
		}
		if idx == -1 {
			if phase == 0 {
				phase++
				log.Printf("recvFiles phase=%d", phase)
				// TODO: send done message
				continue
			}
			break
		}
		log.Printf("receiving file idx=%d: %+v", idx, fileList[idx])
		if err := rt.recvFile1(fileList[idx]); err != nil {
			return err
		}
	}
	log.Printf("recvFiles finished")
	return nil
}

func (rt *recvTransfer) recvFile1(f *file) error {
	localFile, err := rt.openLocalFile(f)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("opening local file failed, continuing: %v", err)
	}
	defer localFile.Close()
	if err := rt.receiveData(f, localFile); err != nil {
		return err
	}
	return nil
}

func (rt *recvTransfer) openLocalFile(f *file) (*os.File, error) {
	local := filepath.Join(rt.dest, f.Name)

	in, err := os.Open(local)
	if err != nil {
		return nil, err
	}

	st, err := in.Stat()
	if err != nil {
		return nil, err
	}

	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory", local)
	}

	if !st.Mode().IsRegular() {
		return nil, nil
	}

	if !rt.opts.PreservePerms {
		// If the file exists already and we are not preserving permissions,
		// then act as though the remote sent us the existing permissions:
		f.Mode = int32(st.Mode().Perm())
	}

	return in, nil
}

// rsync/receiver.c:receive_data
func (rt *recvTransfer) receiveData(f *file, localFile *os.File) error {
	var sh rsync.SumHead
	if err := sh.ReadFrom(rt.conn); err != nil {
		return err
	}

	local := filepath.Join(rt.dest, f.Name)

	log.Printf("creating %s", local)
	out, err := newPendingFile(local)
	if err != nil {
		return err
	}
	defer out.Cleanup()

	h := md4.New()
	binary.Write(h, binary.LittleEndian, rt.seed)

	wr := io.MultiWriter(out, h)

	for {
		token, data, err := rt.recvToken()
		if err != nil {
			return err
		}
		if token == 0 {
			break
		}
		if token > 0 {
			if _, err := wr.Write(data); err != nil {
				return err
			}
			continue
		}
		if localFile == nil {
			return fmt.Errorf("BUG: local file %s not open for copying chunk", local)
		}
		token = -(token + 1)
		offset2 := int64(token) * int64(sh.BlockLength)
		dataLen := sh.BlockLength
		if token == sh.ChecksumCount-1 && sh.RemainderLength != 0 {
			dataLen = sh.RemainderLength
		}
		data = make([]byte, dataLen)
		if _, err := localFile.ReadAt(data, offset2); err != nil {
			return err
		}

		if _, err := wr.Write(data); err != nil {
			return err
		}
	}
	localSum := h.Sum(nil)
	remoteSum := make([]byte, len(localSum))
	if _, err := io.ReadFull(rt.conn.Reader, remoteSum); err != nil {
		return err
	}
	if !bytes.Equal(localSum, remoteSum) {
		return fmt.Errorf("file corruption in %s", f.Name)
	}
	log.Printf("checksum %x matches!", localSum)

	if err := out.CloseAtomicallyReplace(); err != nil {
		return err
	}

	if err := rt.setPerms(f); err != nil {
		return err
	}

	return nil
}
