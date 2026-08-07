package rsync

import "io"

// BothCloser is a wrapper around a ReadCloser and WriteCloser that provides its
// own Close() function which calls Close() on both, the WriteCloser and
// ReadCloser, in order, and returns the first non-nil error.
type BothCloser struct {
	io.ReadCloser
	io.WriteCloser
}

// Close implements io.Closer.
func (b *BothCloser) Close() error {
	wcErr := b.WriteCloser.Close()
	rcErr := b.ReadCloser.Close()
	if wcErr != nil {
		return wcErr
	}
	return rcErr
}
