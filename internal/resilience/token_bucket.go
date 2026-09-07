package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type TokenBucket struct {
	mu         sync.Mutex
	rate       float64
	burst      float64
	tokens     float64
	lastRefill time.Time
}

func NewTokenBucket(ratePerSecond float64, burst int) *TokenBucket {
	if ratePerSecond <= 0 {
		ratePerSecond = 1
	}
	if burst <= 0 {
		burst = 1
	}
	now := time.Now()
	return &TokenBucket{
		rate:       ratePerSecond,
		burst:      float64(burst),
		tokens:     float64(burst),
		lastRefill: now,
	}
}

func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked(time.Now())
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *TokenBucket) Wait(ctx context.Context) error {
	for {
		if b.Allow() {
			return nil
		}

		wait := b.nextWait()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (b *TokenBucket) nextWait() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rate <= 0 {
		return time.Second
	}
	missing := 1 - b.tokens
	if missing <= 0 {
		return 0
	}
	wait := time.Duration((missing / b.rate) * float64(time.Second))
	if wait < 10*time.Millisecond {
		wait = 10 * time.Millisecond
	}
	return wait
}

func (b *TokenBucket) refillLocked(now time.Time) {
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	b.tokens += elapsed * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.lastRefill = now
}

func ErrRateLimited(name string) error {
	return fmt.Errorf("%s rate limited", name)
}
