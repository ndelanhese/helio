package auth

import (
	"sync"
	"testing"
	"time"
)

func TestLimiterBlocksSixthFailureAndNormalizesKey(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	l := NewLimiter([]byte("01234567890123456789012345678901"), func() time.Time { return now })
	for i := 0; i < 5; i++ {
		if allowed, _ := l.Allow("192.0.2.4:1234", "Admin"); !allowed {
			t.Fatalf("attempt %d blocked", i+1)
		}
		l.RecordFailure("192.0.2.4:1234", "Admin")
	}
	allowed, retry := l.Allow("192.0.2.4:9999", "admin")
	if allowed || retry != 15*time.Minute {
		t.Fatalf("sixth attempt allowed=%v retry=%v", allowed, retry)
	}
	l.Reset("192.0.2.4", "ADMIN")
	if allowed, _ := l.Allow("192.0.2.4", "admin"); !allowed {
		t.Fatal("success did not reset bucket")
	}
}

func TestLimiterPrunesExpiredBucketsAndIsRaceSafe(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	l := NewLimiter([]byte("01234567890123456789012345678901"), func() time.Time { return now })
	for i := 0; i < 5; i++ {
		l.RecordFailure("198.51.100.7", "user")
	}
	now = now.Add(15*time.Minute + time.Nanosecond)
	if allowed, _ := l.Allow("198.51.100.7", "user"); !allowed {
		t.Fatal("expired bucket remained blocked")
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.RecordFailure("198.51.100.8:9000", "Admin")
			_, _ = l.Allow("198.51.100.8:9001", "admin")
			l.Reset("198.51.100.8", "ADMIN")
		}()
	}
	wg.Wait()
}
