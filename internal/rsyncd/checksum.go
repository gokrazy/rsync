package rsyncd

import (
	"encoding/binary"

	"github.com/mmcloughlin/md4"
)

func gettag2(s1, s2 uint16) uint16 {
	return (((s1) + (s2)) & 0xFFFF)
}

func gettag(sum uint32) uint16 {
	return gettag2(uint16(sum&0xFFFF), uint16(sum>>16))
}

// signExtend mirrors how C converts from (signed char) to uint32, i.e. using
// sign extension. get_checksum1 treats the buffer as (signed char*) instead of
// (unsigned char*), which likely was not a conscious choice, but here we are.
func signExtend(b byte) uint32 {
	val := uint32(b)
	return uint32(int32(val<<24) >> 24)
}

func getChecksum1(buf []byte) uint32 {
	len := len(buf)
	var s1, s2 uint32
	var i int

	if len > 4 {
		for i = 0; i < (len - 4); i += 4 {
			s2 += 4*(s1+signExtend(buf[i])) +
				3*signExtend(buf[i+1]) +
				2*signExtend(buf[i+2]) +
				signExtend(buf[i+3])
			s1 += signExtend(buf[i+0]) +
				signExtend(buf[i+1]) +
				signExtend(buf[i+2]) +
				signExtend(buf[i+3])
		}
	}
	for ; i < len; i++ {
		s1 += signExtend(buf[i])
		s2 += s1
	}
	return (s1 & 0xffff) + (s2 << 16)
}

func getChecksum2(seed int32, buf []byte) []byte {
	h := md4.New()
	h.Write(buf)
	binary.Write(h, binary.LittleEndian, seed)
	return h.Sum(nil)
}
