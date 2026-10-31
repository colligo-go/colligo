// Package progress draws the bar of a long read on a terminal. It is meant
// for stderr and is never part of a report.
package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/theme"
)

const (
	interval = 100 * time.Millisecond
	barWidth = 24
)

type Bar struct {
	mu    sync.Mutex
	w     io.Writer
	theme theme.Theme
	now   func() time.Time
	last  time.Time
	drawn bool
}

func New(w io.Writer, t theme.Theme) *Bar {
	return &Bar{w: w, theme: t, now: time.Now, last: time.Now()}
}

// Update redraws at most every 100 ms, so a fast read draws nothing.
func (b *Bar) Update(done, total int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if total <= 0 || b.now().Sub(b.last) < interval {
		return
	}
	b.last = b.now()
	filled := int(float64(barWidth) * float64(min(done, total)) / float64(total))
	line := fmt.Sprintf("reading  [%s%s] %3d%%  %s of %s",
		strings.Repeat("#", filled), strings.Repeat("-", barWidth-filled),
		min(done, total)*100/total, format.Size(done), format.Size(total))
	fmt.Fprint(b.w, "\r\x1b[2K"+b.theme.Apply(theme.Dim, line))
	b.drawn = true
}

// Finish erases the bar, so the report starts on a clean line.
func (b *Bar) Finish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.drawn {
		fmt.Fprint(b.w, "\r\x1b[2K")
		b.drawn = false
	}
}
