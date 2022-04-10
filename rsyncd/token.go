package rsyncd

// rsync/token.c:simple_send_token
func (st *sendTransfer) simpleSendToken(ms *mapStruct, token int32, offset int64, n int64) error {
	if n > 0 {
		st.logger.Printf("sending unmatched chunks offset=%d, n=%d", offset, n)
		l := int64(0)
		for l < n {
			n1 := int64(chunkSize)
			if n-l < n1 {
				n1 = n - l
			}

			chunk := ms.ptr(offset+l, int32(n1))

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
func (st *sendTransfer) sendToken(ms *mapStruct, i int32, offset int64, n int64) error {
	// TODO(compression): send deflated token
	return st.simpleSendToken(ms, i, offset, n)
}
