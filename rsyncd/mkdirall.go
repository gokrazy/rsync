package rsyncd

import (
	"os"
	"path/filepath"
	"syscall"
)

// mkdirAll is os.MkdirAll (from go/src/os/path.go), but adapted for *os.Root.
func mkdirAll(root *os.Root, path string, perm os.FileMode) error {
	// Fast path: if we can tell whether path is a directory or file, stop with success or error.
	dir, err := root.Stat(path)
	if err == nil {
		if dir.IsDir() {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: path, Err: syscall.ENOTDIR}
	}

	// Slow path: make sure parent exists and then call Mkdir for path.

	// Extract the parent folder from path by first removing any trailing
	// path separator and then scanning backward until finding a path
	// separator or reaching the beginning of the string.
	i := len(path) - 1
	for i >= 0 && os.IsPathSeparator(path[i]) {
		i--
	}
	for i >= 0 && !os.IsPathSeparator(path[i]) {
		i--
	}
	if i < 0 {
		i = 0
	}

	// If there is a parent directory, and it is not the volume name,
	// recurse to ensure parent directory exists.
	if parent := path[:i]; len(parent) > len(filepath.VolumeName(path)) {
		err := mkdirAll(root, parent, perm)
		if err != nil && !os.IsExist(err) {
			return err
		}
	}

	// Parent now exists; invoke Mkdir and use its result.
	return root.Mkdir(path, perm)
}
