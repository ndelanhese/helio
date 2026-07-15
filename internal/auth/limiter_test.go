package auth

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLimiterBlocksSixthFailureAndNormalizesKey(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	l := NewLimiter([]byte("01234567890123456789012345678901"), func() time.Time { return now })
	for i := 0; i < 5; i++ {
		attempt, _ := l.Admit("192.0.2.4:1234", "Admin")
		if attempt == nil {
			t.Fatalf("attempt %d blocked", i+1)
		}
		attempt.Failure()
	}
	attempt, retry := l.Admit("192.0.2.4:9999", "admin")
	if attempt != nil || retry != 15*time.Minute {
		t.Fatalf("sixth attempt=%v retry=%v", attempt, retry)
	}
}

func TestLimiterPrunesExpiredBucketsAndIsRaceSafe(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	l := NewLimiter([]byte("01234567890123456789012345678901"), func() time.Time { return now })
	for i := 0; i < 5; i++ {
		attempt, _ := l.Admit("198.51.100.7", "user")
		attempt.Failure()
	}
	now = now.Add(15*time.Minute + time.Nanosecond)
	if attempt, _ := l.Admit("198.51.100.7", "user"); attempt == nil {
		t.Fatal("expired bucket remained blocked")
	} else {
		attempt.Cancel()
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			attempt, _ := l.Admit("198.51.100.8:9000", fmt.Sprintf("Admin-%d", i))
			if attempt != nil {
				attempt.Failure()
			}
		}(i)
	}
	wg.Wait()
}

func TestLimiterBoundsChurnAndRetainsBlockedActiveBucket(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	const capacity = 32
	l := newLimiter([]byte("01234567890123456789012345678901"), func() time.Time { return now }, capacity)
	for i := 0; i < loginFailureLimit; i++ {
		attempt, retry := l.Admit("192.0.2.1", "active")
		if attempt == nil || retry != 0 {
			t.Fatalf("active admission %d retry=%v", i+1, retry)
		}
		attempt.Failure()
	}
	for i := 0; i < capacity*10; i++ {
		attempt, _ := l.Admit("198.51.100.2", fmt.Sprintf("churn-%d", i))
		if attempt != nil {
			attempt.Failure()
		}
		if got := l.bucketCount(); got > capacity {
			t.Fatalf("bucket count=%d capacity=%d", got, capacity)
		}
	}
	if attempt, retry := l.Admit("192.0.2.1", "active"); attempt != nil || retry <= 0 {
		t.Fatalf("blocked active bucket lost: attempt=%v retry=%v", attempt, retry)
	}
	now = now.Add(loginWindow + time.Nanosecond)
	if attempt, _ := l.Admit("192.0.2.1", "active"); attempt == nil {
		t.Fatal("expired active bucket remained blocked")
	} else {
		attempt.Cancel()
	}
}

func TestLimiterSixthConcurrentAdmissionIsRateLimited(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	l := NewLimiter([]byte("01234567890123456789012345678901"), func() time.Time { return now })
	start := make(chan struct{})
	release := make(chan struct{})
	results := make(chan bool, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			attempt, _ := l.Admit("203.0.113.9:4000", "Admin")
			results <- attempt != nil
			if attempt != nil {
				<-release
				attempt.Failure()
			}
		}()
	}
	close(start)
	admitted := 0
	for i := 0; i < 12; i++ {
		if <-results {
			admitted++
		}
	}
	close(release)
	wg.Wait()
	if admitted != loginFailureLimit {
		t.Fatalf("concurrent admitted=%d want=%d", admitted, loginFailureLimit)
	}
}

func TestLimiterAdmissionCompletionIsIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	l := NewLimiter([]byte("01234567890123456789012345678901"), func() time.Time { return now })
	attempt, _ := l.Admit("203.0.113.10", "Admin")
	attempt.Failure()
	attempt.Failure()
	for i := 1; i < loginFailureLimit; i++ {
		attempt, _ = l.Admit("203.0.113.10", "Admin")
		if attempt == nil {
			t.Fatalf("double-counted completion before failure %d", i+1)
		}
		attempt.Failure()
	}
	if attempt, _ := l.Admit("203.0.113.10", "Admin"); attempt != nil {
		t.Fatal("sixth attempt admitted")
	}
}
