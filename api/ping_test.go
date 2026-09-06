package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// pingRequest builds an unauthenticated request for /ping, carrying no
// Authorization header -- pingMiddleware must never require one.
func pingRequest(method, target, remoteAddr string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remoteAddr
	return req
}

// useFreshPingRateLimiter swaps the package pingRateLimiter for a fresh
// instance with the given budget, isolating each test's count from any
// other test's rate-limit state, and restores the original afterwards.
func useFreshPingRateLimiter(t *testing.T, budget int) {
	t.Helper()
	orig := pingRateLimiter
	pingRateLimiter = NewRateLimiter(budget)
	t.Cleanup(func() { pingRateLimiter = orig })
}

// GET gets 204, empty body, and all three required headers -- no
// Authorization needed.
func TestPingGetReturnsNoContent(t *testing.T) {
	useFreshPingRateLimiter(t, DefaultPingRateLimitRequests)

	handler := pingMiddleware(handlePing)
	rec := httptest.NewRecorder()
	handler(rec, pingRequest(http.MethodGet, "/ping", "203.0.113.5:1234"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET /ping = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if body := rec.Body.String(); body != "" {
		t.Errorf("GET /ping body = %q, want empty", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	if got := rec.Header().Get("X-RateLimit-Limit"); got != strconv.Itoa(DefaultPingRateLimitRequests) {
		t.Errorf("X-RateLimit-Limit = %q, want %q", got, strconv.Itoa(DefaultPingRateLimitRequests))
	}
}

// HEAD behaves the same as GET: 204, empty body.
func TestPingHeadReturnsNoContent(t *testing.T) {
	useFreshPingRateLimiter(t, DefaultPingRateLimitRequests)

	handler := pingMiddleware(handlePing)
	rec := httptest.NewRecorder()
	handler(rec, pingRequest(http.MethodHead, "/ping", "203.0.113.5:1234"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("HEAD /ping = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if body := rec.Body.String(); body != "" {
		t.Errorf("HEAD /ping body = %q, want empty", body)
	}
}

// Any other method is refused with 405 and the required Allow header --
// still without needing auth (the middleware never checks for it).
func TestPingPostReturnsMethodNotAllowed(t *testing.T) {
	useFreshPingRateLimiter(t, DefaultPingRateLimitRequests)

	handler := pingMiddleware(handlePing)
	rec := httptest.NewRecorder()
	handler(rec, pingRequest(http.MethodPost, "/ping", "203.0.113.5:1234"))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /ping = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
	}
}

// The consumer's cache-busting `?t=<ts>` query string must be ignored
// entirely -- still a plain 204, not reflected anywhere in the response.
func TestPingIgnoresQueryString(t *testing.T) {
	useFreshPingRateLimiter(t, DefaultPingRateLimitRequests)

	handler := pingMiddleware(handlePing)
	rec := httptest.NewRecorder()
	handler(rec, pingRequest(http.MethodGet, "/ping?t=1699999999", "203.0.113.5:1234"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET /ping?t=... = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

// D6: when RemoteAddr is loopback (the shape of a request proxied in from
// Caddy on the same host), the client IP bucket comes from the FIRST entry
// of X-Forwarded-For, so distinct browsers behind the proxy do not share one
// 127.0.0.1 bucket.
func TestPingHonoursForwardedForBehindLoopback(t *testing.T) {
	const budget = 2
	useFreshPingRateLimiter(t, budget)

	handler := pingMiddleware(handlePing)

	// Two different XFF IPs, each making `budget` requests through the same
	// loopback RemoteAddr: both must be allowed in full, proving they land
	// in separate buckets rather than sharing 127.0.0.1's.
	for _, xff := range []string{"198.51.100.10", "198.51.100.20"} {
		for i := 0; i < budget; i++ {
			req := pingRequest(http.MethodGet, "/ping", "127.0.0.1:9999")
			req.Header.Set("X-Forwarded-For", xff+", 10.0.0.1")
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("XFF=%s request %d/%d = %d, want %d (separate bucket per forwarded IP)",
					xff, i+1, budget, rec.Code, http.StatusNoContent)
			}
		}
	}
}

// D6, the other half: when RemoteAddr is NOT loopback, X-Forwarded-For must
// be ignored -- otherwise any direct caller could spoof its own rate-limit
// bucket by setting an arbitrary XFF header.
func TestPingIgnoresForwardedForWhenNotLoopback(t *testing.T) {
	const budget = 2
	useFreshPingRateLimiter(t, budget)

	handler := pingMiddleware(handlePing)

	// budget requests direct from 203.0.113.5, each claiming a DIFFERENT
	// spoofed XFF value. If XFF were honoured they would land in separate
	// buckets and none would be throttled; since it must be ignored, they
	// share 203.0.113.5's bucket and the request after budget is throttled.
	for i := 0; i < budget; i++ {
		req := pingRequest(http.MethodGet, "/ping", "203.0.113.5:4242")
		req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(100+i))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("request %d/%d = %d, want %d", i+1, budget, rec.Code, http.StatusNoContent)
		}
	}

	req := pingRequest(http.MethodGet, "/ping", "203.0.113.5:4242")
	req.Header.Set("X-Forwarded-For", "198.51.100.200")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d = %d, want %d -- spoofed XFF must not open a new bucket",
			budget+1, rec.Code, http.StatusTooManyRequests)
	}
}

// The Nth+1 request in a window is throttled with 429, Retry-After, and
// X-RateLimit-Remaining: 0 -- same shape as every authenticated route.
func TestPingRateLimitThrottles(t *testing.T) {
	const budget = 3
	useFreshPingRateLimiter(t, budget)

	handler := pingMiddleware(handlePing)
	const remoteAddr = "203.0.113.9:5555"

	for i := 0; i < budget; i++ {
		rec := httptest.NewRecorder()
		handler(rec, pingRequest(http.MethodGet, "/ping", remoteAddr))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("request %d/%d = %d, want %d within budget %d", i+1, budget, rec.Code, http.StatusNoContent, budget)
		}
	}

	rec := httptest.NewRecorder()
	handler(rec, pingRequest(http.MethodGet, "/ping", remoteAddr))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d = %d, want %d once the budget is spent", budget+1, rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("throttled X-RateLimit-Remaining = %q, want %q", got, "0")
	}
	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("throttled Retry-After = %q, want an integer count of seconds: %v", rec.Header().Get("Retry-After"), err)
	}
	if retryAfter < 1 {
		t.Errorf("throttled Retry-After = %d, want >= 1", retryAfter)
	}
}

// /health, and by extension every other route, must stay behind
// authMiddleware -- adding /ping must not loosen anything else.
func TestHealthStillRequiresAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	rec := httptest.NewRecorder()

	authMiddleware(handleHealth)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /health without a key = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
