package receiver

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
)

// openTempFileRoot creates a randomly named file in root and returns an open
// handle. It is similar to os.CreateTemp except that the directory must be
// given, the file permissions can be controlled and patterns in the name are
// not supported.  The name is always suffixed with a random number.
func openTempFileRoot(root *os.Root, name string, perm os.FileMode) (string, *os.File, error) {
	prefix := name

	for attempt := 0; ; {
		// Generate a reasonably random name which is unlikely to already
		// exist. O_EXCL ensures that existing files generate an error.
		name := prefix + strconv.FormatInt(rand.Int64(), 10)

		f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
		if !os.IsExist(err) {
			return name, f, err
		}

		if attempt++; attempt > 10000 {
			return "", nil, &os.PathError{
				Op:   "tempfile",
				Path: name,
				Err:  os.ErrExist,
			}
		}
	}
}

type pendingFile struct {
	root    *os.Root
	tmpname string
	fn      string
	f       *os.File
	sync    bool
	renamed bool
}

func newPendingFile(root *os.Root, fn string, sync bool) (*pendingFile, error) {
	tmpname, f, err := openTempFileRoot(root, "."+filepath.Base(fn), 0o600)
	if err != nil {
		return nil, err
	}
	return &pendingFile{
		root:    root,
		tmpname: tmpname,
		fn:      fn,
		f:       f,
		sync:    sync,
	}, nil
}

func (p *pendingFile) Name() string {
	return p.fn
}

func (p *pendingFile) Write(buf []byte) (n int, _ error) {
	return p.f.Write(buf)
}

func (p *pendingFile) CloseAtomicallyReplace() error {
	if p.sync {
		// fsync was requested
		if err := p.f.Sync(); err != nil {
			return err
		}
	}
	if err := p.f.Close(); err != nil {
		return err
	}
	if err := p.root.Rename(p.tmpname, p.fn); err != nil {
		return err
	}
	p.renamed = true
	return nil
}

func (p *pendingFile) Cleanup() error {
	if p.renamed {
		return nil // CloseAtomicallyReplace succeeded.
	}
	err := p.f.Close()
	if err := p.root.Remove(p.tmpname); err != nil {
		return err
	}
	return err
}

func (p *pendingFile) KeepAsPartial(partialName string) error {
	if p.renamed {
		return nil // CloseAtomicallyReplace succeeded.
	}
	err := p.f.Close()
	if err := p.root.MkdirAll(filepath.Dir(partialName), 0700); err != nil {
		return err
	}
	if err := p.root.Rename(p.tmpname, partialName); err != nil {
		return err
	}
	return err
}
