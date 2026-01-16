package receiver

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/mmcloughlin/md4"
)

// rsync/receiver.c:recv_files
func (rt *Transfer) RecvFiles(fileList []*File) error {
	phase := 0
	for {
		idx, err := rt.Conn.ReadInt32()
		if err != nil {
			return err
		}
		if idx == -1 {
			if phase == 0 {
				phase++
				if rt.Opts.DebugGTE(rsyncopts.DEBUG_RECV, 1) {
					rt.Logger.Printf("recvFiles phase=%d", phase)
				}
				// TODO: send done message
				continue
			}
			break
		}
		if rt.Opts.DebugGTE(rsyncopts.DEBUG_RECV, 1) {
			rt.Logger.Printf("receiving file idx=%d: %+v", idx, fileList[idx])
		}
		if rt.Opts.Progress {
			fmt.Fprintln(rt.Env.Stdout, fileList[idx].Name)
		}
		if err := rt.recvFile1(fileList[idx]); err != nil {
			return err
		}
	}
	if rt.Opts.DebugGTE(rsyncopts.DEBUG_RECV, 1) {
		rt.Logger.Printf("recvFiles finished")
	}
	return nil
}

func (rt *Transfer) recvFile1(f *File) error {
	if rt.Opts.DryRun {
		if !rt.Opts.Server {
			fmt.Fprintln(rt.Env.Stdout, f.Name)
		}
		return nil
	}

	localFile, err := rt.openLocalFile(f)
	if err != nil && !os.IsNotExist(err) {
		rt.Logger.Printf("opening local file failed, continuing: %v", err)
	}
	defer localFile.Close()
	if err := rt.receiveData(f, localFile); err != nil {
		return err
	}
	return nil
}

func (rt *Transfer) openLocalFile(f *File) (*os.File, error) {
	in, err := rt.DestRoot.Open(f.Name)
	if err != nil {
		return nil, err
	}

	st, err := in.Stat()
	if err != nil {
		return nil, err
	}

	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory", filepath.Join(rt.Dest, f.Name))
	}

	if !st.Mode().IsRegular() {
		return nil, nil
	}

	if !rt.Opts.PreservePerms {
		// If the file exists already and we are not preserving permissions,
		// then act as though the remote sent us the existing permissions:
		f.Mode = int32(st.Mode().Perm())
	}

	return in, nil
}

// rsync/receiver.c:receive_data
func (rt *Transfer) receiveData(f *File, localFile *os.File) error {
	rt.Progress.Reset(uint64(f.Length))
	var sh rsync.SumHead
	if err := sh.ReadFrom(rt.Conn); err != nil {
		return err
	}

	if rt.Opts.DebugGTE(rsyncopts.DEBUG_DELTASUM, 1) {
		local := filepath.Join(rt.Dest, f.Name)
		rt.Logger.Printf("creating %s", local)
	}
	out, err := newPendingFile(rt.DestRoot, f.Name)
	if err != nil {
		return err
	}
	defer out.Cleanup()

	h := md4.New()
	binary.Write(h, binary.LittleEndian, rt.Seed)

	wr := io.MultiWriter(out, h)

	offset := 0
	for {
		token, data, err := rt.recvToken()
		if err != nil {
			return err
		}
		if token == 0 {
			break
		}
		if rt.Opts.Progress && !rt.Opts.Server {
			rt.Progress.MaybeShow(uint64(offset), false)
			if offset == 0 {
				defer func() {
					rt.Progress.MaybeShow(uint64(offset), true)
				}()
			}
		}
		if token > 0 {
			n, err := wr.Write(data)
			if err != nil {
				return err
			}
			offset += n
			continue
		}
		if localFile == nil {
			return fmt.Errorf("BUG: local file %s not open for copying chunk", out.Name())
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

		n, err := wr.Write(data)
		if err != nil {
			return err
		}
		offset += n
	}
	localSum := h.Sum(nil)
	remoteSum := make([]byte, len(localSum))
	if _, err := io.ReadFull(rt.Conn.Reader, remoteSum); err != nil {
		return err
	}
	if !bytes.Equal(localSum, remoteSum) {
		return fmt.Errorf("file corruption in %s", f.Name)
	}
	if rt.Opts.DebugGTE(rsyncopts.DEBUG_DELTASUM, 1) {
		rt.Logger.Printf("checksum %x matches!", localSum)
	}

	if err := out.CloseAtomicallyReplace(); err != nil {
		return err
	}

	if err := rt.setPerms(f, fs.FileMode(f.Mode)); err != nil {
		return err
	}

	return nil
}
