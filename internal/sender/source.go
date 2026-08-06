package sender

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
)

// FileSource is the interface which the gokrazy rsync sender uses
// to discover and read files. This allows working with rsync modules
// that are backed by an actual file system (*os.Root) or fs.FS.
type FileSource interface {
	// FS returns the underlying fs.FS for use with fs.WalkDir.
	FS() fs.FS

	// Open opens a file. Returns error if file does not implement io.Seeker.
	Open(name string) (File, error)

	// Readlink reads a symlink target. Needs fs.ReadLinkFS.
	Readlink(name string) (string, error)

	Close() error
}

type File interface {
	fs.File
	io.Seeker
}

type osRootSource struct {
	root *os.Root
}

func newOSRootSource(root *os.Root) FileSource {
	return &osRootSource{root: root}
}

func (s *osRootSource) FS() fs.FS                            { return s.root.FS() }
func (s *osRootSource) Open(name string) (File, error)       { return s.root.Open(name) }
func (s *osRootSource) Readlink(name string) (string, error) { return s.root.Readlink(name) }
func (s *osRootSource) Close() error                         { return s.root.Close() }

// singleFileSource implements FileSource.
type singleFileSource struct {
	path string
	info fs.FileInfo
}

func newSingleFileSource(path string, info fs.FileInfo) *singleFileSource {
	return &singleFileSource{
		path: path,
		info: info,
	}
}

func (s *singleFileSource) FS() fs.FS                         { return &singleFileFS{s} }
func (s *singleFileSource) Open(_ string) (File, error)       { return os.Open(s.path) }
func (s *singleFileSource) Readlink(_ string) (string, error) { return os.Readlink(s.path) }
func (s *singleFileSource) Close() error                      { return nil }

type singleFileFS struct {
	sfs *singleFileSource
}

func (f *singleFileFS) Open(name string) (fs.File, error)     { return os.Open(f.sfs.path) }
func (f *singleFileFS) Stat(name string) (fs.FileInfo, error) { return f.sfs.info, nil }

// fsSource wraps an fs.FS to implement FileSource.
type fsSource struct {
	fsys fs.FS
}

// NewFSSource creates a FileSource from an fs.FS.
//
// Files returned by the fs.FS must implement io.Seeker,
// otherwise Open will fail.
//
// The fs.FS should implement ReadLinkFS,
// otherwise working with symlinks will fail.
func NewFSSource(fsys fs.FS) FileSource {
	return &fsSource{fsys: fsys}
}

func (s *fsSource) FS() fs.FS { return s.fsys }

func (s *fsSource) Open(name string) (File, error) {
	f, err := s.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	sf, ok := f.(File) // checks for io.Seeker
	if !ok {
		f.Close()
		return nil, fmt.Errorf("open %s: fs.File must implement io.Seeker", name)
	}
	return sf, nil
}

func (s *fsSource) Readlink(name string) (string, error) {
	if rl, ok := s.fsys.(fs.ReadLinkFS); ok {
		return rl.ReadLink(name)
	}
	return "", fmt.Errorf("readlink %s: fs.FS does not implement fs.ReadLinkFS", name)
}

func (s *fsSource) Close() error { return nil }

// subSource wraps a FileSource and serves the specified subtree,
// like fs.Sub() does for fs.FS.
type subSource struct {
	underlying FileSource
	subdir     string
	fsys       fs.FS
}

func newSubSource(underlying FileSource, subdir string) (FileSource, error) {
	fsys, err := fs.Sub(underlying.FS(), subdir)
	if err != nil {
		return nil, err
	}
	return &subSource{
		underlying: underlying,
		subdir:     subdir,
		fsys:       fsys,
	}, nil
}

func (s *subSource) FS() fs.FS { return s.fsys }

func (s *subSource) Open(name string) (File, error) {
	return s.underlying.Open(path.Join(s.subdir, name))
}

func (s *subSource) Readlink(name string) (string, error) {
	return s.underlying.Readlink(path.Join(s.subdir, name))
}

func (s *subSource) Close() error { return nil }
