package progress

import "time"

func AdvanceClock(b *Bar, milliseconds int) {
	current := time.Unix(1_000_000, 0)
	b.last = current
	b.now = func() time.Time { return current.Add(time.Duration(milliseconds) * time.Millisecond) }
}
