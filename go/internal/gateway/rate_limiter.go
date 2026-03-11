package gateway

import (
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/infera/infera/go/internal/auth"
)

const maxRetryAfterSeconds = int(^uint32(0) >> 1)

// RateLimiterConfig configures the rate limiter.
type RateLimiterConfig struct {
	// RequestsPerMinute is the max requests per key per minute.
	RequestsPerMinute int
	// BurstSize is the max burst allowed above the steady rate.
	BurstSize int
	// CleanupInterval controls how often stale buckets are removed.
	CleanupInterval time.Duration
}

// DefaultRateLimiterConfig returns production defaults.
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
		CleanupInterval:   5 * time.Minute,
	}
}

// tokenBucket is a simple token bucket for one key.
type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func newTokenBucket(maxTokens float64, refillRate float64) *tokenBucket {
	return &tokenBucket{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (b *tokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// retryAfterSeconds returns how long until a token is available.
func (b *tokenBucket) retryAfterSeconds() int {
	if b.tokens >= 1 {
		return 0
	}
	if b.refillRate <= 0 {
		return maxRetryAfterSeconds
	}
	deficit := 1 - b.tokens
	seconds := deficit / b.refillRate
	return int(seconds) + 1
}

// RateLimiter provides per-key rate limiting using token buckets.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	config  RateLimiterConfig
	stopCh  chan struct{}
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config RateLimiterConfig) *RateLimiter {
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = DefaultRateLimiterConfig().CleanupInterval
	}

	rl := &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		config:  config,
		stopCh:  make(chan struct{}),
	}

	// Background cleanup of stale buckets
	go rl.cleanup()

	return rl
}

// Allow checks if a request from the given key should be allowed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[key]
	if !exists {
		refillRate := float64(rl.config.RequestsPerMinute) / 60.0
		maxTokens := math.Max(1.0, float64(rl.config.BurstSize)+refillRate) // burst + 1 second of tokens
		bucket = newTokenBucket(maxTokens, refillRate)
		rl.buckets[key] = bucket
	}

	return bucket.allow()
}

// RetryAfter returns the number of seconds to wait before retrying for a given key.
func (rl *RateLimiter) RetryAfter(key string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[key]
	if !exists {
		return 0
	}
	return bucket.retryAfterSeconds()
}

// Stop stops the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for key, bucket := range rl.buckets {
				// Remove buckets that haven't been used for 2x cleanup interval
				if now.Sub(bucket.lastRefill) > 2*rl.config.CleanupInterval {
					delete(rl.buckets, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCh:
			return
		}
	}
}

// RateLimitMiddleware returns HTTP middleware that rate-limits by API key.
// It extracts the key from Authorization or X-API-Key headers.
func RateLimitMiddleware(rl *RateLimiter) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Extract key identifier for rate limiting
			key := r.Header.Get("Authorization")
			if key == "" {
				key = r.Header.Get("X-API-Key")
			}
			if key == "" {
				if record := auth.KeyFromContext(r.Context()); record != nil {
					key = record.ID
					if key == "" {
						key = record.Name
					}
				}
			}
			if key == "" {
				// No key = let auth middleware handle rejection
				next(w, r)
				return
			}

			if !rl.Allow(key) {
				retryAfter := rl.RetryAfter(key)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"error":{"type":"rate_limited","message":"Rate limit exceeded. Retry after %d seconds."}}`, retryAfter)
				return
			}

			next(w, r)
		}
	}
}
