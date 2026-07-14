package auth

import (
	"container/list"
	"crypto/hmac"
	"crypto/sha256"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureLimit      = 5
	loginWindow            = 15 * time.Minute
	defaultLimiterCapacity = 4096
)

type failureBucket struct {
	key             [sha256.Size]byte
	failures        int
	pending         int
	expiresAt       time.Time
	expiryElement   *list.Element
	evictionElement *list.Element
}

// Limiter bounds attacker-controlled state and accounts for concurrent login
// work at admission time. Expiry pruning and deterministic eviction are O(1)
// amortized; no operation scans the bucket map.
type Limiter struct {
	mu        sync.Mutex
	secret    []byte
	now       func() time.Time
	capacity  int
	buckets   map[[sha256.Size]byte]*failureBucket
	expiry    list.List
	evictable [loginFailureLimit + 1]list.List
}

func NewLimiter(secret []byte, clock func() time.Time) *Limiter {
	return newLimiter(secret, clock, defaultLimiterCapacity)
}

func newLimiter(secret []byte, clock func() time.Time, capacity int) *Limiter {
	if len(secret) < 32 {
		panic("auth: limiter secret must contain at least 256 bits")
	}
	if clock == nil {
		clock = time.Now
	}
	if capacity < loginFailureLimit {
		panic("auth: limiter capacity is too small")
	}
	return &Limiter{
		secret: append([]byte(nil), secret...), now: clock, capacity: capacity,
		buckets: make(map[[sha256.Size]byte]*failureBucket, capacity),
	}
}

// Admission is a single reserved login attempt. Exactly one of Failure,
// Success, or Cancel should complete it; sync.Once makes double completion safe.
type Admission struct {
	limiter *Limiter
	key     [sha256.Size]byte
	bucket  *failureBucket
	once    sync.Once
}

// Admit atomically reserves one of the five permitted attempts for a key.
func (l *Limiter) Admit(remoteAddr, username string) (*Admission, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.pruneExpired(now)
	key := l.key(remoteAddr, username)
	if bucket := l.buckets[key]; bucket != nil {
		if bucket.failures+bucket.pending >= loginFailureLimit {
			return nil, retryDuration(bucket.expiresAt, now)
		}
		l.removeEvictable(bucket)
		bucket.pending++
		return &Admission{limiter: l, key: key, bucket: bucket}, 0
	}
	if !l.makeRoom() {
		// Every retained bucket currently has password work in flight. Refuse
		// additional work rather than exceeding the hard memory bound.
		return nil, time.Second
	}
	bucket := &failureBucket{key: key, pending: 1, expiresAt: now.Add(loginWindow)}
	bucket.expiryElement = l.expiry.PushBack(bucket)
	l.buckets[key] = bucket
	return &Admission{limiter: l, key: key, bucket: bucket}, 0
}

func (a *Admission) Failure() { a.complete(admissionFailure) }
func (a *Admission) Success() { a.complete(admissionSuccess) }
func (a *Admission) Cancel()  { a.complete(admissionCancel) }

type admissionResult uint8

const (
	admissionFailure admissionResult = iota
	admissionSuccess
	admissionCancel
)

func (a *Admission) complete(result admissionResult) {
	if a == nil || a.limiter == nil {
		return
	}
	a.once.Do(func() {
		l := a.limiter
		l.mu.Lock()
		defer l.mu.Unlock()
		now := l.now()
		l.pruneExpired(now)
		current := l.buckets[a.key]
		if result == admissionSuccess {
			if current != nil {
				l.deleteBucket(current)
			}
			return
		}
		if current == a.bucket {
			if current.pending > 0 {
				current.pending--
			}
			if result == admissionFailure && current.failures < loginFailureLimit {
				current.failures++
			}
			if current.pending == 0 {
				if current.failures == 0 {
					l.deleteBucket(current)
				} else {
					l.addEvictable(current)
				}
			}
			return
		}
		// A success or expiry may have removed this admission's generation.
		// A failure completing afterward starts/counts in the current window.
		if result == admissionFailure {
			l.recordLateFailure(a.key, now)
		}
	})
}

func (l *Limiter) recordLateFailure(key [sha256.Size]byte, now time.Time) {
	if bucket := l.buckets[key]; bucket != nil {
		l.removeEvictable(bucket)
		if bucket.failures < loginFailureLimit {
			bucket.failures++
		}
		if bucket.pending == 0 {
			l.addEvictable(bucket)
		}
		return
	}
	if !l.makeRoom() {
		return
	}
	bucket := &failureBucket{key: key, failures: 1, expiresAt: now.Add(loginWindow)}
	bucket.expiryElement = l.expiry.PushBack(bucket)
	l.buckets[key] = bucket
	l.addEvictable(bucket)
}

func (l *Limiter) pruneExpired(now time.Time) {
	for element := l.expiry.Front(); element != nil; element = l.expiry.Front() {
		bucket := element.Value.(*failureBucket)
		if now.Before(bucket.expiresAt) {
			return
		}
		l.deleteBucket(bucket)
	}
}

func (l *Limiter) makeRoom() bool {
	if len(l.buckets) < l.capacity {
		return true
	}
	for failures := 0; failures <= loginFailureLimit; failures++ {
		if element := l.evictable[failures].Front(); element != nil {
			l.deleteBucket(element.Value.(*failureBucket))
			return true
		}
	}
	return false
}

func (l *Limiter) addEvictable(bucket *failureBucket) {
	if bucket.evictionElement == nil {
		bucket.evictionElement = l.evictable[bucket.failures].PushBack(bucket)
	}
}

func (l *Limiter) removeEvictable(bucket *failureBucket) {
	if bucket.evictionElement != nil {
		l.evictable[bucket.failures].Remove(bucket.evictionElement)
		bucket.evictionElement = nil
	}
}

func (l *Limiter) deleteBucket(bucket *failureBucket) {
	if l.buckets[bucket.key] != bucket {
		return
	}
	delete(l.buckets, bucket.key)
	if bucket.expiryElement != nil {
		l.expiry.Remove(bucket.expiryElement)
		bucket.expiryElement = nil
	}
	l.removeEvictable(bucket)
}

func (l *Limiter) bucketCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneExpired(l.now())
	return len(l.buckets)
}

func retryDuration(expiresAt, now time.Time) time.Duration {
	retry := expiresAt.Sub(now)
	if retry < time.Second {
		return time.Second
	}
	return retry
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
