package rsyncd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash"
	"log"
	"os"

	"github.com/gokrazy/rsync"
	"github.com/mmcloughlin/md4"
)

// rsync/match.c:hash_search
func (st *sendTransfer) hashSearch(targets []target, tagTable map[uint16]int, head rsync.SumHead, fileIndex int32, fl file) error {
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

	if err := head.WriteTo(st.conn); err != nil {
		return err
	}

	// sum_init()
	h := md4.New()
	binary.Write(h, binary.LittleEndian, st.seed)

	// The following quotes are citations from
	// https://www.samba.org/~tridge/phd_thesis.pdf, section 3.2.6 The
	// signature search algorithm (PDF page 64).

	// “Once the sorted signature table and the index table have been formed the
	// signature search process can begin. For each byte offset in a_i the fast
	// signature is computed, along with the 16 bit hash of the fast
	// signature. The 16 bit hash is then used to lookup the signature index,
	// giving the index in the signature table of the first fast signature with
	// that hash.”

	var k int
	var sum uint32
	var s1, s2 uint16
	var chunk []byte
	var offset int64
	end := fi.Size() + 1 - head.Sums[len(head.Sums)-1].Len
	log.Printf("last block len=%d, end=%d", head.Sums[len(head.Sums)-1].Len, end)

	readChunk := func() error {
		k = int(head.BlockLength)
		if remaining := int(fi.Size() - offset); remaining < k {
			k = remaining
		}

		buf := make([]byte, k)
		// TODO: here and below: do we need io.ReadFull?
		n, err := f.ReadAt(buf, offset)
		// log.Printf("reading chunk at offset=%d with len=%d (ret=%d)", offset, len(buf), n)
		if err != nil {
			return err
		}
		chunk = buf[:n]
		sum = getChecksum1(chunk)
		s1 = uint16(sum & 0xFFFF)
		s2 = uint16(sum >> 16)
		return nil
	}
	if err := readChunk(); err != nil {
		return err
	}

	tagHits := 0
Outer:
	for {
		tag := gettag2(s1, s2)
		var sum2 []byte
		doneCsum2 := false
		j, ok := tagTable[tag]
		if ok {
			// “A linear search is then performed through the signature table, stopping
			// when an entry is found with a 16 bit hash which doesn’t match. For each
			// entry the current 32 bit fast signature is compared to the entry in the
			// signature table, and if that matches then the full 128 bit strong
			// signature is computed at the current byte offset and compared to the
			// strong signature in the signature table”
			sum = (uint32(s1) & 0xFFFF) | (uint32(s2) << 16)
			tagHits++
			for ; j < int(head.ChecksumCount) && targets[j].tag == tag; j++ {
				i := targets[j].index
				if sum != head.Sums[i].Sum1 {
					continue
				}

				l := int64(head.BlockLength)
				if v := fi.Size() - offset; v < l {
					l = v
				}
				if l != head.Sums[i].Len {
					continue
				}

				// log.Printf("potential match at %d target=%d %d sum=%08x", offset, j, i, sum)

				if !doneCsum2 {
					buf := make([]byte, l)
					n, err := f.ReadAt(buf, offset)
					if err != nil {
						return err
					}
					sum2 = getChecksum2(st.seed, buf[:n])
					doneCsum2 = true
				}

				if local, remote := sum2[:head.ChecksumLength], head.Sums[i].Sum2[:head.ChecksumLength]; !bytes.Equal(local, remote) {
					log.Printf("false alarm: local %x, remote %x", local, remote)
					//falseAlarms++
					continue
				}

				// TODO(optimization): tridge rsync locates adjacent matches
				// here for better run-length encoding, but I’m not sure where
				// (if at all) we currently use run-length encoding:
				// https://github.com/WayneD/rsync/commit/923fa978088f4c044eec528d9472962d9c9d13c3

				// “If the strong signature is found to match then A emits a
				// token telling B that a match was found and which block in bi
				// was matched12. The search then continues at the byte after
				// the matching block.”

				if err := st.matched(h, f, head, offset, i); err != nil {
					return err
				}

				offset += head.Sums[i].Len
				if err := readChunk(); err != nil {
					return fmt.Errorf("readChunk: %v", err)
				}

				if offset >= end {
					break
				}

				continue Outer

				// rsync doesn’t read the next chunk (offset+sums[i].len),
				// rsync starts reading one byte before the next chunk
				// (offset+sums[i].len-1), because the code path starting at
				// “null_tag” removes the chunk’s first byte and adds the
				// next byte after the chunk.
				offset += head.Sums[i].Len - 1
				if err := readChunk(); err != nil {
					return fmt.Errorf("readChunk: %v", err)
				}
				break
			}
		}
		// null_tag
		offset++
		if offset >= end {
			break
		}

		if err := readChunk(); err != nil {
			return fmt.Errorf("readChunk: %v", err)
		}

		continue Outer

		// TODO: make the rolling checksum below work:

		//log.Printf("null_tag, k=%d", k)
		readk := k + 1
		add := k < int(fi.Size()-offset)
		if !add {
			readk--
		}

		if readk > 0 {
			buf := make([]byte, readk)
			n, err := f.ReadAt(buf, offset)
			//log.Printf("ReadAt(offset=%d, len=%d, ret=%d)", offset, len(buf), n)
			if err != nil {
				return fmt.Errorf("[resync] ReadAt(%v, len=%d): %v", offset, len(buf), err)
			}
			update := buf[:n]
			s1 -= uint16(update[0])
			s2 -= uint16(k) * uint16(update[0])

			if add {
				s1 += uint16(update[k])
				s2 += s1
			} else {
				log.Printf("WARNING: not enough bytes available")
				k--
			}

			// TODO: match early
			// if err := c.matched(h, f, head, offset-int64(head.BlockLength), -2); err != nil {
			// 	return err
			// }
		}

		offset++
		if offset >= end {
			break
		}
	}

	if err := st.matched(h, f, head, fi.Size(), -1); err != nil {
		return err
	}

	{
		sum := h.Sum(nil)
		log.Printf("sum: %x (len = %d)", sum, len(sum))
		if _, err := st.conn.Writer.Write(sum); err != nil {
			return err
		}
	}

	return nil

}

// rsync/match.c:matched
func (st *sendTransfer) matched(h hash.Hash, f *os.File, head rsync.SumHead, offset int64, i int32) error {
	n := offset - st.lastMatch

	transmitAccumulated := i < 0

	if !transmitAccumulated {
		log.Printf("match at offset=%d last_match=%d i=%d len=%d n=%d",
			offset, st.lastMatch, i, head.Sums[i].Len, n)
	}

	l := int64(0)
	if !transmitAccumulated {
		l = head.Sums[i].Len
	}

	if err := st.sendToken(f, i, st.lastMatch, n, l); err != nil {
		return fmt.Errorf("sendToken: %v", err)
	}
	// TODO: data_transfer += n;

	if !transmitAccumulated {
		// stats.matched_data += s->sums[i].len;
		n += head.Sums[i].Len
	}

	for j := int64(0); j < n; j += chunkSize {
		n1 := int64(chunkSize)
		if n-j < n1 {
			n1 = n - j
		}

		buf := make([]byte, n1)
		n, err := f.ReadAt(buf, st.lastMatch+j)
		if err != nil {
			return fmt.Errorf("ReadAt(%d): %v", st.lastMatch+j, err)
		}
		chunk := buf[:n]
		h.Write(chunk)
	}

	if !transmitAccumulated {
		st.lastMatch = offset + head.Sums[i].Len
	} else {
		st.lastMatch = offset
	}
	return nil
}
