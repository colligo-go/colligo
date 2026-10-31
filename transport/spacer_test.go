package transport

import (
	"sort"
	"sync"
	"testing"
	"time"
)

func TestSpacerNeverExceedsTheCeilingInAnyWindow(t *testing.T) {
	const perSecond = 4
	const interval = time.Second / perSecond
	start := time.Unix(1_700_000_000, 0)
	s := newSpacer()

	var mu sync.Mutex
	var slots []time.Time
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				slot := s.reserve("www.ebi.ac.uk", interval, start)
				mu.Lock()
				slots = append(slots, slot)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	sort.Slice(slots, func(i, j int) bool { return slots[i].Before(slots[j]) })
	if len(slots) != 160 {
		t.Fatalf("got %d slots", len(slots))
	}
	for i := range slots {
		windowEnd := slots[i].Add(time.Second)
		inWindow := 0
		for j := i; j < len(slots) && slots[j].Before(windowEnd); j++ {
			inWindow++
		}
		if inWindow > perSecond {
			t.Fatalf("%d requests in the second starting at slot %d, ceiling is %d", inWindow, i, perSecond)
		}
	}
}

func TestSpacerKeepsDomainsIndependent(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := newSpacer()
	s.reserve("a.example", time.Second, now)
	if slot := s.reserve("b.example", time.Second, now); !slot.Equal(now) {
		t.Fatalf("second domain was delayed to %v", slot)
	}
}

func TestSpacerDoesNotAccumulateCreditWhileIdle(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := newSpacer()
	s.reserve("a.example", time.Second, now)
	later := now.Add(time.Hour)
	first := s.reserve("a.example", time.Second, later)
	second := s.reserve("a.example", time.Second, later)
	if !first.Equal(later) || second.Sub(first) != time.Second {
		t.Fatalf("slots after idle: %v then %v", first, second)
	}
}
