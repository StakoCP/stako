// SPDX-License-Identifier: MIT
package api

import (
	"sync"
	"time"
)

type bucket struct {
	failures  int
	resetAt   time.Time
	lockedOut time.Time
}

// RateLimiter tracks failed attempts per key (e.g. IP or username) with progressive lockout.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   int
	window  time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		limit:   limit,
		window:  window,
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		return true
	}
	if !b.lockedOut.IsZero() && now.Before(b.lockedOut) {
		return false
	}
	if now.After(b.resetAt) {
		delete(rl.buckets, key)
		return true
	}
	return b.failures < rl.limit
}

func (rl *RateLimiter) RecordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok || now.After(b.resetAt) {
		b = &bucket{resetAt: now.Add(rl.window)}
		rl.buckets[key] = b
	}
	b.failures++
	if b.failures >= rl.limit {
		b.lockedOut = now.Add(rl.window)
	}
}

func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, key)
}
