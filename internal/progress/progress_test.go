package progress

import (
	"bytes"
	"testing"
	"time"
)

func TestProgress(t *testing.T) {
	now := time.Now()
	var buf bytes.Buffer
	p := NewPrinter(&buf, func() time.Time {
		return now
	})
	p.Reset(1234)
	p.Show(0, false)
	if got, want := buf.String(), "              0   0%    0.00kB/s    0:00:00"; got != want {
		t.Errorf("progress.Show(0) = %q, want %q", got, want)
	}
	now = now.Add(1 * time.Second)
	buf.Reset()
	p.Show(617, false)
	if got, want := buf.String(), "\r            617  50%    0.60kB/s    0:00:01"; got != want {
		t.Errorf("progress.Show(617) = %q, want %q", got, want)
	}
}
