package collector

import (
	"testing"
	"time"
)

func TestBackoffDoublesAndCapsAfterJitter(t *testing.T) {
	b := NewBackoff(time.Second, time.Minute, func(delay time.Duration) time.Duration {
		return delay + time.Second
	})

	for i, want := range []time.Duration{2 * time.Second, 3 * time.Second, 5 * time.Second, 9 * time.Second, 17 * time.Second, 33 * time.Second, time.Minute} {
		if got := b.Next(); got != want {
			t.Fatalf("attempt %d: delay=%s, want %s", i+1, got, want)
		}
	}
}

func TestBackoffResetRestartsSequence(t *testing.T) {
	b := NewBackoff(time.Second, time.Minute, nil)
	_ = b.Next()
	_ = b.Next()
	b.Reset()
	if got := b.Next(); got != time.Second {
		t.Fatalf("delay after reset=%s, want 1s", got)
	}
}
