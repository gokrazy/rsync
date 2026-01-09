package progress

import (
	"fmt"
	"io"
	"time"
)

type progressAt struct {
	when   time.Time
	offset uint64
}

type Printer struct {
	// config
	out io.Writer
	now func() time.Time

	// state
	first   bool
	size    uint64
	history [5]progressAt
	oldest  int // index into history
}

func NewPrinter(out io.Writer, now func() time.Time) Printer {
	p := Printer{
		out: out,
		now: now,
	}
	n := now()
	for i := range 5 {
		p.history[i] = progressAt{
			when:   n,
			offset: 0,
		}
	}
	return p
}

func (p *Printer) Reset(size uint64) {
	now := p.now()
	p.size = size
	p.first = true
	for i := range 5 {
		p.history[i] = progressAt{
			when:   now,
			offset: 0,
		}
	}
}

func (p *Printer) MaybeShow(offset uint64, last bool) {
	newest := p.oldest
	if newest == 0 {
		newest = 4
	} else {
		newest--
	}
	now := p.now()
	if !last && now.Sub(p.history[newest].when) < 1*time.Second {
		return
	}
	p.Show(offset, last)
}

func (p *Printer) Show(offset uint64, last bool) {
	now := p.now()
	newest := p.oldest
	p.oldest = (p.oldest + 1) % 5
	p.history[newest] = progressAt{
		when:   now,
		offset: offset,
	}

	pct := int(float64(offset) / float64(p.size) * 100)

	oldestOffset := p.history[p.oldest].offset
	diff := now.Sub(p.history[p.oldest].when).Seconds()
	if diff == 0 {
		diff = 1
	}
	rate := float64(offset-oldestOffset) / diff
	var remainSec float64 // seconds
	if rate > 0 {
		remainSec = float64(p.size-offset) / rate
	}

	rate /= 1024
	unit := "kB/s"
	switch {
	case rate > 1024*1024:
		rate /= 1024 * 1024
		unit = "GB/s"
	case rate > 1024:
		rate /= 1024
		unit = "MB/s"
	}

	remaining := "  ??:??:??"
	if remainSec >= 0 && remainSec <= 9999*3600 {
		remaining = fmt.Sprintf("%4d:%02d:%02d",
			int(remainSec/3600),
			int(remainSec/60)%60,
			int(remainSec)%60)
	}

	if p.first {
		p.first = false
	} else {
		p.out.Write([]byte{'\r'})
	}
	fmt.Fprintf(p.out, "%15d %3d%% %7.2f%s %s", offset, pct, rate, unit, remaining)
	if last {
		// TODO: show where we are within the file list
		// (number of files transferred vs. number of files total)
		p.out.Write([]byte{'\n'})
	}
}
