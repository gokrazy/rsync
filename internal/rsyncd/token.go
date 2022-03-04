package rsyncd

import (
	"fmt"
	"log"
	"os"
)

// rsync/token.c:simple_send_token
func (st *sendTransfer) simpleSendToken(f *os.File, token int32, offset int64, n int64) error {
	if n > 0 {
		log.Printf("sending unmatched chunks")
		l := int64(0)
		for l < n {
			n1 := int64(chunkSize)
			if n-l < n1 {
				n1 = n - l
			}

			buf := make([]byte, n1)
			n, err := f.ReadAt(buf, offset+l)
			if err != nil {
				return fmt.Errorf("ReadAt(%v): %v", offset+l, err)
			}
			chunk := buf[:n]

			if err := st.conn.WriteInt32(int32(n1)); err != nil {
				return err
			}

			if _, err := st.conn.Writer.Write(chunk); err != nil {
				return err
			}

			l += n1
		}
	}
	if token != -2 {
		return st.conn.WriteInt32(-(token + 1))
	}
	return nil
}

// rsync/token.c:send_token
func (st *sendTransfer) sendToken(f *os.File, i int32, offset int64, n int64) error {
	// TODO(compression): send deflated token
	return st.simpleSendToken(f, i, offset, n)
}
