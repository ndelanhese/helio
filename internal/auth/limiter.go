package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureLimit = 5
	loginWindow       = 15 * time.Minute
)

type failureBucket struct {
	failures  int
	expiresAt time.Time
}

type Limiter struct {
	mu      sync.Mutex
	secret  []byte
	now     func() time.Time
	buckets map[[sha256.Size]byte]failureBucket
}

func NewLimiter(secret []byte, clock func() time.Time) *Limiter {
	ownedSecret := append([]byte(nil), secret...)
	if clock == nil {
		clock = time.Now
	}
	return &Limiter{secret: ownedSecret, now: clock, buckets: make(map[[sha256.Size]byte]failureBucket)}
}

func (l *Limiter) Allow(remoteAddr, username string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.prune(now)
	bucket, ok := l.buckets[l.key(remoteAddr, username)]
	if !ok || bucket.failures < loginFailureLimit {
		return true, 0
	}
	retry := bucket.expiresAt.Sub(now)
	if retry < time.Second {
		retry = time.Second
	}
	return false, retry
}

func (l *Limiter) RecordFailure(remoteAddr, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.prune(now)
	key := l.key(remoteAddr, username)
	bucket, exists := l.buckets[key]
	if !exists {
		bucket.expiresAt = now.Add(loginWindow)
	}
	bucket.failures++
	l.buckets[key] = bucket
}

func (l *Limiter) Reset(remoteAddr, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(l.now())
	delete(l.buckets, l.key(remoteAddr, username))
}

func (l *Limiter) prune(now time.Time) {
	for key, bucket := range l.buckets {
		if !now.Before(bucket.expiresAt) {
			delete(l.buckets, key)
		}
	}
}

func (l *Limiter) key(remoteAddr, username string) [sha256.Size]byte {
	h := hmac.New(sha256.New, l.secret)
	_, _ = h.Write([]byte(normalizeIP(remoteAddr)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(username))))
	var key [sha256.Size]byte
	copy(key[:], h.Sum(nil))
	return key
}

func normalizeIP(remoteAddr string) string {
	value := strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	return strings.ToLower(value)
}
