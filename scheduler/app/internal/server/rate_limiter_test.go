package server

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPRateLimiterBlocksAndRefills(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	limiter := newHTTPRateLimiter()
	limiter.now = func() time.Time {
		return now
	}
	rule := rateLimitRule{name: "test", rate: 1, burst: 2}

	if ok, _ := limiter.allow(rule, "client"); !ok {
		t.Fatal("first request should be allowed")
	}
	if ok, _ := limiter.allow(rule, "client"); !ok {
		t.Fatal("second request should be allowed")
	}
	if ok, retryAfter := limiter.allow(rule, "client"); ok || retryAfter <= 0 {
		t.Fatalf("third request should be blocked with retry-after, ok=%v retry=%s", ok, retryAfter)
	}

	now = now.Add(time.Second)
	if ok, _ := limiter.allow(rule, "client"); !ok {
		t.Fatal("request after refill should be allowed")
	}
}

func TestWorkerQueryKeyUsesWorkerID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/pull?worker_id=0xABC", nil)
	req.RemoteAddr = "192.0.2.1:1234"

	if got := workerQueryKey(req); got != "worker:0xabc" {
		t.Fatalf("unexpected worker key: %q", got)
	}
}
