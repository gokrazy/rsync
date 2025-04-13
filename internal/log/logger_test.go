package log_test

import (
	"bytes"
	"fmt"

	"github.com/gokrazy/rsync/internal/log"
)

type fakeLogger struct {
	out *bytes.Buffer
}

var _ log.Logger = (*fakeLogger)(nil)

func (f *fakeLogger) Printf(msg string, a ...any) {
	fmt.Fprintf(f.out, msg, a...)
}

func (f *fakeLogger) Output(calldepth int, s string) error {
	fmt.Fprintf(f.out, "%s", s)
	return nil
}
