// Package ratelimit provides a keyed in-memory token-bucket limiter.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type entry struct {
	lim  *rate.Limiter
	seen time.Time
}

// Limiter holds one token bucket per key and drops idle keys.
type Limiter struct {
	rate      rate.Limit
	burst     int
	mu        sync.Mutex
	keys      map[string]*entry
	now       func() time.Time
	lastSweep time.Time
}

func New(r rate.Limit, burst int) *Limiter {
	return &Limiter{rate: r, burst: burst, keys: map[string]*entry{}, now: time.Now}
}

// SetClock overrides the time source (tests).
func (l *Limiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	l.now = now
	l.mu.Unlock()
}

// Len returns the number of tracked keys.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.keys)
}

// Allow consumes a token for key and reports whether one was available.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	e, ok := l.keys[key]
	if !ok {
		e = &entry{lim: rate.NewLimiter(l.rate, l.burst)}
		l.keys[key] = e
	}
	e.seen = now
	return e.lim.AllowN(now, 1)
}

// sweep removes keys idle for longer than a full refill; caller holds mu.
func (l *Limiter) sweep(now time.Time) {
	ttl := time.Minute
	if l.rate > 0 {
		if full := time.Duration(float64(l.burst) / float64(l.rate) * float64(time.Second)); full > ttl {
			ttl = full
		}
	}
	if now.Sub(l.lastSweep) < ttl {
		return
	}
	l.lastSweep = now
	for k, e := range l.keys {
		if now.Sub(e.seen) >= ttl {
			delete(l.keys, k)
		}
	}
}
