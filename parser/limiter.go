package parser

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RateLimiter struct {
	mu sync.Mutex

	// Minimum time between requests.
	lastRequest time.Time

	// Set when server responds with 429.
	blockedUntil time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{}
}

// Wait blocks until it is safe to make the next request.
func (r *RateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()

	now := time.Now()

	// Normal delay between requests: 1.5-2.0 seconds.
	delay := time.Duration(1500+rand.Intn(501)) * time.Millisecond

	// Time required because of the normal rate limit.
	wait := delay - now.Sub(r.lastRequest)
	if wait < 0 {
		wait = 0
	}

	// If the server previously returned 429, respect Retry-After.
	blockedWait := time.Until(r.blockedUntil)
	if blockedWait > wait {
		wait = blockedWait
	}

	// Reserve the next request slot before unlocking.
	r.lastRequest = now.Add(wait)

	r.mu.Unlock()

	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BlockFor prevents all requests for the specified duration.
func (r *RateLimiter) BlockFor(duration time.Duration) {
	if duration <= 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	until := time.Now().Add(duration)

	// Never shorten an existing cooldown.
	if until.After(r.blockedUntil) {
		r.blockedUntil = until
	}
}

// RetryAfter extracts Retry-After from a 429 response.
//
// Retry-After can be either:
//
//	Retry-After: 5
//
// or:
//
//	Retry-After: Wed, 21 Oct 2015 07:28:00 GMT
func RetryAfter(resp *http.Response) time.Duration {
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))

	if value == "" {
		// Server did not tell us how long to wait.
		// Use a conservative fallback.
		return 5 * time.Second
	}

	// Retry-After: seconds
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 1 * time.Second
		}

		return time.Duration(seconds) * time.Second
	}

	// Retry-After: HTTP date
	if retryAt, err := http.ParseTime(value); err == nil {
		delay := time.Until(retryAt)

		if delay > 0 {
			return delay
		}
	}

	// Invalid/unrecognized Retry-After.
	return 5 * time.Second
}
