package receivermaincmd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/gokrazy/rsync"
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
		// receive_data()
		if err := rt.receiveData(fileList[idx]); err != nil {
			return err
		}
	}
	log.Printf("recvFiles finished")
	return nil
}

// rsync/receiver.c:receive_data
func (rt *recvTransfer) receiveData(f *file) error {
	var sh rsync.SumHead
	if err := sh.ReadFrom(rt.conn); err != nil {
		return err
	}
	log.Printf("sum head: %+v", sh)

	h := md4.New()
	binary.Write(h, binary.LittleEndian, rt.seed)

	var offset int64
	for {
		token, data, err := rt.recvToken()
		if err != nil {
			return err
		}
		if token == 0 {
			break
		}
		if token > 0 {
			log.Printf("data recv %d at %d", token, offset)
			h.Write(data)
			// TODO: write to file
			offset += int64(token)
			continue
		}
		return fmt.Errorf("re-using existing file parts not yet implemented")
	}
	localSum := h.Sum(nil)
	log.Printf("reading %d checksum bytes", len(localSum))
	remoteSum := make([]byte, len(localSum))
	if _, err := io.ReadFull(rt.conn.Reader, remoteSum); err != nil {
		return err
	}
	if !bytes.Equal(localSum, remoteSum) {
		return fmt.Errorf("file corruption in %s", f.Name)
	}
	log.Printf("checksum %x matches!", localSum)
	return nil
}
