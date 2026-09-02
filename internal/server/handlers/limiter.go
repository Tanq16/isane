package handlers

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	freeAccountAttempts = 5
	freeSourceAttempts  = 20

	backoffBase    = time.Second
	backoffCeiling = 15 * time.Minute
	attemptTTL     = time.Hour
)

type limiter struct {
	free  int
	mu    sync.Mutex
	seen  map[string]*attempts
	swept time.Time
}

type attempts struct {
	failures int
	until    time.Time
	touched  time.Time
}

func newLimiter(free int) *limiter {
	return &limiter{free: free, seen: make(map[string]*attempts)}
}

func (l *limiter) retryAfter(keys ...string) time.Duration {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	var longest time.Duration
	for _, key := range keys {
		a := l.seen[key]
		if a == nil {
			continue
		}
		if wait := a.until.Sub(now); wait > longest {
			longest = wait
		}
	}
	return longest
}

func (l *limiter) fail(keys ...string) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	for _, key := range keys {
		a := l.seen[key]
		if a == nil {
			a = &attempts{}
			l.seen[key] = a
		}
		a.failures++
		a.touched = now
		if a.failures > l.free {
			a.until = now.Add(min(backoffBase<<(a.failures-l.free-1), backoffCeiling))
		}
	}
}

func (l *limiter) succeed(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		delete(l.seen, key)
	}
}

func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < attemptTTL {
		return
	}
	l.swept = now
	for key, a := range l.seen {
		if now.Sub(a.touched) > attemptTTL {
			delete(l.seen, key)
		}
	}
}

func writeRetryAfter(w http.ResponseWriter, wait time.Duration, message string) {
	seconds := max(int(wait.Round(time.Second)/time.Second), 1)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	WriteJSON(w, http.StatusTooManyRequests, errorBody{Error: message})
}
