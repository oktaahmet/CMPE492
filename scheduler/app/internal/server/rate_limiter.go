package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateLimitRule struct {
	name     string
	rate     float64
	burst    float64
	keyFunc  func(*http.Request) string
	retryFor time.Duration
}

type rateLimitBucket struct {
	tokens   float64
	lastSeen time.Time
}

type httpRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateLimitBucket
	now     func() time.Time
}

// newHTTPRateLimiter is intentionally in-memory. It protects the current
// single scheduler process; multiple scheduler replicas would need a shared
// backend such as Redis to enforce one cluster-wide budget.
func newHTTPRateLimiter() *httpRateLimiter {
	return &httpRateLimiter{
		buckets: map[string]rateLimitBucket{},
		now:     time.Now,
	}
}

// The app uses separate buckets per endpoint group instead of one global
// budget. A noisy runtime page should not consume a worker's /api/pull budget,
// and worker polling should not lock an operator out of admin endpoints.
func authRateLimitRule() rateLimitRule {
	return perMinuteRateLimit("auth", 30, 10, nil)
}

func adminRateLimitRule() rateLimitRule {
	return perMinuteRateLimit("admin", 60, 20, nil)
}

func readRateLimitRule(name string) rateLimitRule {
	return perMinuteRateLimit(name, 300, 60, nil)
}

func artifactRateLimitRule(name string) rateLimitRule {
	return perMinuteRateLimit(name, 600, 120, nil)
}

func workerRateLimitRule(name string) rateLimitRule {
	return perMinuteRateLimit(name, 180, 60, nil)
}

// Pull is keyed by worker_id so several legitimate workers behind the same NAT
// do not throttle each other while polling for assignments.
func pullRateLimitRule() rateLimitRule {
	return perMinuteRateLimit("pull", 240, 80, workerQueryKey)
}

func perMinuteRateLimit(name string, requestsPerMinute int, burst int, keyFunc func(*http.Request) string) rateLimitRule {
	return rateLimitRule{
		name:     name,
		rate:     float64(requestsPerMinute) / 60,
		burst:    float64(burst),
		keyFunc:  keyFunc,
		retryFor: time.Minute,
	}
}

func (l *httpRateLimiter) allow(rule rateLimitRule, requestKey string) (bool, time.Duration) {
	if l == nil || rule.rate <= 0 || rule.burst <= 0 {
		return true, 0
	}

	now := l.now()
	key := rule.name + ":" + requestKey

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.buckets[key]
	if bucket.lastSeen.IsZero() {
		bucket.tokens = rule.burst
		bucket.lastSeen = now
	} else {
		elapsed := now.Sub(bucket.lastSeen).Seconds()
		if elapsed > 0 {
			bucket.tokens = minFloat(rule.burst, bucket.tokens+elapsed*rule.rate)
			bucket.lastSeen = now
		}
	}

	if bucket.tokens >= 1 {
		bucket.tokens -= 1
		l.buckets[key] = bucket
		l.cleanupLocked(now)
		return true, 0
	}

	l.buckets[key] = bucket
	wait := time.Duration((1 - bucket.tokens) / rule.rate * float64(time.Second))
	if wait < time.Second {
		wait = time.Second
	}
	if rule.retryFor > 0 && wait > rule.retryFor {
		wait = rule.retryFor
	}
	return false, wait
}

// cleanupLocked only runs when the map gets large. Most deployments will stay
// well below this, so we avoid scanning the bucket map on every request.
func (l *httpRateLimiter) cleanupLocked(now time.Time) {
	const maxIdle = 15 * time.Minute
	if len(l.buckets) < 4096 {
		return
	}
	for key, bucket := range l.buckets {
		if now.Sub(bucket.lastSeen) > maxIdle {
			delete(l.buckets, key)
		}
	}
}

func withRateLimit(limiter *httpRateLimiter, rule rateLimitRule, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r)
		if rule.keyFunc != nil {
			if customKey := strings.TrimSpace(rule.keyFunc(r)); customKey != "" {
				key = customKey
			}
		}
		allowed, retryAfter := limiter.allow(rule, key)
		if !allowed {
			w.Header().Set("Retry-After", retryAfterSeconds(retryAfter))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

func clientIP(r *http.Request) string {
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		for _, part := range strings.Split(forwardedFor, ",") {
			if ip := strings.TrimSpace(part); ip != "" {
				return ip
			}
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func workerQueryKey(r *http.Request) string {
	workerID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("worker_id")))
	if workerID == "" {
		return clientIP(r)
	}
	return "worker:" + workerID
}

func retryAfterSeconds(d time.Duration) string {
	seconds := int(d.Round(time.Second).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconvItoa(seconds)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
