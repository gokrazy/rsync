package log_test

import (
	"bytes"
	"fmt"
	golog "log"
	"testing"

	"github.com/gokrazy/rsync/internal/log"
)

// make sure we won't panic for calling directly
func Test_DefaultLoggerUsage(t *testing.T) {
	log.Printf("foo")
	log.Printf("foo: %s", "bar")
}

type fakeLogger struct {
	out *bytes.Buffer
}

var _ log.Logger = (*fakeLogger)(nil)

func (f *fakeLogger) Printf(msg string, a ...interface{}) {
	fmt.Fprintf(f.out, msg, a...)
}

func Test_SetLogger(t *testing.T) {
	defer func() {
		log.SetLogger(golog.Default())
	}()

	l := &fakeLogger{out: new(bytes.Buffer)}

	log.SetLogger(l)
	log.Printf("foo")
	log.Printf("foo: %s", "bar")

	if v := l.out.String(); v != "foofoo: bar" {
		t.Errorf("unexpected log output: %s", v)
	}
}
