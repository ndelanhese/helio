package collector

import "time"

// Jitter adjusts a retry delay. The result is capped by Backoff's maximum.
type Jitter func(time.Duration) time.Duration

// Backoff produces a capped exponential retry sequence.
type Backoff struct {
	minimum time.Duration
	maximum time.Duration
	jitter  Jitter
	next    time.Duration
}

func NewBackoff(minimum, maximum time.Duration, jitter Jitter) *Backoff {
	return &Backoff{minimum: minimum, maximum: maximum, jitter: jitter, next: minimum}
}

func (b *Backoff) Next() time.Duration {
	delay := b.next
	if b.next < b.maximum {
		if b.next > b.maximum/2 {
			b.next = b.maximum
		} else {
			b.next *= 2
		}
	}
	if b.jitter != nil {
		delay = b.jitter(delay)
	}
	if delay > b.maximum {
		return b.maximum
	}
	if delay < 0 {
		return 0
	}
	return delay
}

func (b *Backoff) Reset() {
	b.next = b.minimum
}
