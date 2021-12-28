package rsyncd

import (
	"bytes"
	"encoding/binary"
	"io"
)

const (
	msgData  uint8 = 0
	msgInfo  uint8 = 2
	msgError uint8 = 1
)

type multiplexWriter struct {
	underlying io.Writer
}

func (w *multiplexWriter) Write(p []byte) (n int, err error) {
	return w.WriteMsg(msgData, p)
}

func (w *multiplexWriter) WriteMsg(tag uint8, p []byte) (n int, err error) {
	const mplexBase = 7
	header := uint32(mplexBase+tag)<<24 | uint32(len(p))
	// log.Printf("len %d (hex %x)", len(p), uint32(len(p)))
	// log.Printf("header=%v (%x)", header, header)
	if err := binary.Write(w.underlying, binary.LittleEndian, header); err != nil {
		return 0, err
	}
	return w.underlying.Write(p)
}

type rsyncBuffer struct {
	// buf.Write() never fails, making for a convenient API.
	buf bytes.Buffer
}

func (b *rsyncBuffer) writeByte(data byte) {
	binary.Write(&b.buf, binary.LittleEndian, data)
}

func (b *rsyncBuffer) writeInt32(data int32) {
	binary.Write(&b.buf, binary.LittleEndian, data)
}

func (b *rsyncBuffer) writeInt64(data int64) {
	// send as a 32-bit integer if possible
	if data <= 0x7FFFFFFF && data >= 0 {
		b.writeInt32(int32(data))
		return
	}
	// otherwise, send -1 followed by the 64-bit integer
	b.writeInt32(-1)
	binary.Write(&b.buf, binary.LittleEndian, data)
}

func (b *rsyncBuffer) writeString(data string) {
	io.WriteString(&b.buf, data)
}

type rsyncConn struct {
	seed int32
	wr   io.Writer
	rd   io.Reader

	lastMatch int64
}

func (c *rsyncConn) writeByte(data byte) error {
	return binary.Write(c.wr, binary.LittleEndian, data)
}

func (c *rsyncConn) writeInt32(data int32) error {
	return binary.Write(c.wr, binary.LittleEndian, data)
}

func (c *rsyncConn) writeInt64(data int64) error {
	// send as a 32-bit integer if possible
	if data <= 0x7FFFFFFF && data >= 0 {
		return c.writeInt32(int32(data))
	}
	// otherwise, send -1 followed by the 64-bit integer
	if err := c.writeInt32(-1); err != nil {
		return err
	}
	return binary.Write(c.wr, binary.LittleEndian, data)
}

func (c *rsyncConn) writeString(data string) error {
	_, err := io.WriteString(c.wr, data)
	return err
}

func (c *rsyncConn) readByte() (byte, error) {
	var buf [1]byte
	if _, err := io.ReadFull(c.rd, buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func (c *rsyncConn) readInt32() (int32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(c.rd, buf[:]); err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(buf[:])), nil
}

type sumHead struct {
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

	Sums []sumBuf
}

func (c *rsyncConn) readSumHead() (sumHead, error) {
	var s sumHead
	var err error
	s.ChecksumCount, err = c.readInt32()
	if err != nil {
		return s, err
	}

	s.BlockLength, err = c.readInt32()
	if err != nil {
		return s, err
	}

	s.ChecksumLength, err = c.readInt32()
	if err != nil {
		return s, err
	}

	s.RemainderLength, err = c.readInt32()
	if err != nil {
		return s, err
	}
	return s, nil
}

func (c *rsyncConn) writeSumHead(s sumHead) error {
	var buf rsyncBuffer
	buf.writeInt32(s.ChecksumCount)
	buf.writeInt32(s.BlockLength)
	buf.writeInt32(s.ChecksumLength)
	buf.writeInt32(s.RemainderLength)
	return c.writeString(buf.buf.String())
}
