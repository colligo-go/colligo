package transport

import (
	"sync"
	"time"
)

type spacer struct {
	mu   sync.Mutex
	next map[string]time.Time
}

func newSpacer() *spacer {
	return &spacer{next: map[string]time.Time{}}
}

func (s *spacer) reserve(host string, interval time.Duration, now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot := now
	if queued, ok := s.next[host]; ok && queued.After(now) {
		slot = queued
	}
	s.next[host] = slot.Add(interval)
	return slot
}
