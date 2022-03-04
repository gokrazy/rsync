package rsync

import "github.com/gokrazy/rsync/internal/rsyncwire"

// rsync/rsync.h:struct sum_buf
type SumBuf struct {
	Offset int64
	Len    int64
	Index  int32
	Sum1   uint32
	Sum2   [16]byte
}

// TODO: remove connection.go:sumHead in favor of this type
type SumHead struct {
	// “number of blocks” (openrsync)
	// “how many chunks” (rsync)
	ChecksumCount int32

	// “block length in the file” (openrsync)
	// maximum (1 << 29) for older rsync, (1 << 17) for newer
	BlockLength int32

	// “long checksum length” (openrsync)
	ChecksumLength int32

	// “terminal (remainder) block length” (openrsync)
	// RemainderLength is flength % BlockLength
	RemainderLength int32

	Sums []SumBuf
}

func (sh *SumHead) ReadFrom(c *rsyncwire.Conn) error {
	var err error
	sh.ChecksumCount, err = c.ReadInt32()
	if err != nil {
		return err
	}

	sh.BlockLength, err = c.ReadInt32()
	if err != nil {
		return err
	}

	sh.ChecksumLength, err = c.ReadInt32()
	if err != nil {
		return err
	}

	sh.RemainderLength, err = c.ReadInt32()
	if err != nil {
		return err
	}
	return nil
}

func (sh *SumHead) WriteTo(c *rsyncwire.Conn) error {
	var buf rsyncwire.Buffer
	buf.WriteInt32(sh.ChecksumCount)
	buf.WriteInt32(sh.BlockLength)
	buf.WriteInt32(sh.ChecksumLength)
	buf.WriteInt32(sh.RemainderLength)
	return c.WriteString(buf.String())
}
