// Package testlogger contains a helper to put a stdout/stderr output stream of
// a subprocess onto the testing package's t.Log().
package testlogger

import (
	"bufio"
	"io"
	"sync"
	"testing"
)

type Logger struct {
	tb      testing.TB
	writer  *io.PipeWriter
	scanner *bufio.Scanner
}

func New(tb testing.TB) *Logger {
	r, w := io.Pipe()
	tl := &Logger{
		tb:      tb,
		writer:  w,
		scanner: bufio.NewScanner(r),
	}
	var wg sync.WaitGroup
	wg.Add(1)
	tb.Cleanup(func() {
		w.Close()
		// tl.scanner.Scan() will return false,
		// tl.scanner.Err() will return nil.

		// Ensure the goroutine below is done
		// to prevent data races in tb.Log()
		wg.Wait()
	})
	go func() {
		defer wg.Done()
		for tl.scanner.Scan() {
			tb.Log(tl.scanner.Text())
		}
		if err := tl.scanner.Err(); err != nil {
			tb.Log(err)
		}
	}()
	return tl
}

// Write implements io.Writer.
func (lw Logger) Write(p []byte) (n int, err error) {
	return lw.writer.Write(p)
}
