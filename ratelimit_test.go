package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIPFromXRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "203.0.113.50")
	r.RemoteAddr = "192.168.1.1:12345"

	got := clientIP(r)
	if got != "203.0.113.50" {
		t.Fatalf("expected 203.0.113.50, got %s", got)
	}
}

func TestClientIPFallbackRemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "198.51.100.7:54321"

	got := clientIP(r)
	if got != "198.51.100.7" {
		t.Fatalf("expected 198.51.100.7, got %s", got)
	}
}

func TestClientIPv6MaskedToSlash64(t *testing.T) {
	tests := []struct {
		name    string
		inputA  string
		inputB  string
		sameKey bool
	}{
		{
			name:    "same /64 prefix",
			inputA:  "2001:db8:1234:5678::1",
			inputB:  "2001:db8:1234:5678::ffff",
			sameKey: true,
		},
		{
			name:    "different /64 prefix",
			inputA:  "2001:db8:1234:5678::1",
			inputB:  "2001:db8:1234:5679::1",
			sameKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rA := httptest.NewRequest(http.MethodGet, "/", nil)
			rA.Header.Set("X-Real-IP", tt.inputA)
			rB := httptest.NewRequest(http.MethodGet, "/", nil)
			rB.Header.Set("X-Real-IP", tt.inputB)

			ipA := clientIP(rA)
			ipB := clientIP(rB)

			if tt.sameKey && ipA != ipB {
				t.Fatalf("expected same key for %s and %s, got %s and %s", tt.inputA, tt.inputB, ipA, ipB)
			}
			if !tt.sameKey && ipA == ipB {
				t.Fatalf("expected different keys for %s and %s, both got %s", tt.inputA, tt.inputB, ipA)
			}
		})
	}
}

func TestRateLimitMiddlewareRejectsOverLimit(t *testing.T) {
	rl := newIPRateLimiter(1, 1)
	handler := rateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should pass
	r1 := httptest.NewRequest(http.MethodGet, "/", nil)
	r1.RemoteAddr = "10.0.0.1:1234"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	// Second request should be rate limited
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "10.0.0.1:1234"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", w2.Code)
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	rl := newIPRateLimiter(10, 10)

	// Access a limiter to create an entry
	rl.getLimiter("10.0.0.1")

	rl.mu.Lock()
	if len(rl.visitors) != 1 {
		rl.mu.Unlock()
		t.Fatalf("expected 1 visitor, got %d", len(rl.visitors))
	}
	// Backdate the entry so it appears expired
	rl.visitors["10.0.0.1"].lastSeen = time.Now().Add(-20 * time.Minute)
	rl.mu.Unlock()

	rl.cleanup(10 * time.Minute)

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if len(rl.visitors) != 0 {
		t.Fatalf("expected 0 visitors after cleanup, got %d", len(rl.visitors))
	}
}
