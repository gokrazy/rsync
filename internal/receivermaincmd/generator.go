package receivermaincmd

import (
	"fmt"
	"log"
	"os"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/rsyncwire"
)

// rsync/generator.c:generate_files()
func (rt *recvTransfer) generateFiles(fileList []*file) error {
	phase := 0
	for idx, f := range fileList {
		// TODO: use a copy of f with .Mode |= S_IWUSR for directories, so
		// that we can create files within all directories.

		if err := rt.recvGenerator(idx, f); err != nil {
			return err
		}
	}
	phase++
	log.Printf("generateFiles phase=%d", phase)
	if err := rt.conn.WriteInt32(-1); err != nil {
		return err
	}

	// TODO: re-do any files that failed
	phase++
	log.Printf("generateFiles phase=%d", phase)
	if err := rt.conn.WriteInt32(-1); err != nil {
		return err
	}

	log.Printf("generateFiles finished")
	return nil
}

// rsync/generator.c:recv_generator
func (rt *recvTransfer) recvGenerator(idx int, f *file) error {
	if rt.listOnly() {
		fmt.Fprintf(rt.env.stdout, "%s %11.0f %s %s\n",
			f.FileMode().String(),
			float64(f.Length), // TODO: rsync prints decimal separators
			f.ModTime.Format("2006/01/02 15:04:05"),
			f.Name)
		return nil
	}
	log.Printf("recv_generator(f=%+v)", f)
	st, err := os.Lstat(f.Name)
	if os.IsNotExist(err) {
		mode := f.Mode & rsync.S_IFMT
		if mode == rsync.S_IFDIR {
			log.Printf("skipping directory")
			return nil
		}
		log.Printf("requesting: %s", f.Name)
		if err := rt.conn.WriteInt32(int32(idx)); err != nil {
			return err
		}
		var buf rsyncwire.Buffer
		buf.WriteInt32(0)
		buf.WriteInt32(0)
		buf.WriteInt32(0)
		buf.WriteInt32(0)
		if err := rt.conn.WriteString(buf.String()); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	log.Printf("st: %+v", st)
	return fmt.Errorf("dealing with existing files not yet implemented")
}
