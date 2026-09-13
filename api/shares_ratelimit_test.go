package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// useTestAPIKey sets the global apiKey for one test and restores it
// afterwards -- authMiddleware/authMiddlewareWithLimiter compare the
// Authorization header against this.
func useTestAPIKey(t *testing.T) string {
	t.Helper()
	orig := apiKey
	apiKey = "test-key-for-ratelimit"
	t.Cleanup(func() { apiKey = orig })
	return apiKey
}

// authorizedRequest builds a request that passes authMiddleware's auth
// check and is attributable to remoteAddr for rate limiting (getClientIP
// strips the port, so remoteAddr must carry one).
func authorizedRequest(method, target, key, remoteAddr string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.RemoteAddr = remoteAddr
	return req
}

// (a) /shares' own budget (SharesRateLimitRequests = 60) must comfortably
// tolerate the 2-3s polling cadence it was sized for: 31 consecutive
// requests from one IP, well over the DEFAULT 20/60s ceiling, none
// throttled. A fresh RateLimiter is used (not the package global
// sharesRateLimiter) so this test cannot interfere with, or be interfered
// by, any other test's rate-limit state -- no real sleeps needed, since the
// limiter counts requests within its window rather than timing them.
func TestSharesRateLimitAllowsPollingCadence(t *testing.T) {
	key := useTestAPIKey(t)
	useTempSharelogDir(t) // avoid handleShares reading an unrelated real dir
	resetShareCache(t)

	limiter := NewRateLimiter(DefaultSharesRateLimitRequests)
	handler := authMiddlewareWithLimiter(limiter, handleShares)

	const remoteAddr = "203.0.113.10:5555"
	const requests = 31 // > RateLimitRequests(20), still < SharesRateLimitRequests(60)
	for i := 0; i < requests; i++ {
		req := authorizedRequest(http.MethodGet, "/shares", key, remoteAddr)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d/%d throttled at 429, want all %d within the /shares %d-per-window budget to pass",
				i+1, requests, requests, DefaultSharesRateLimitRequests)
		}
	}
}

// (b) Giving /shares its own budget must NOT loosen the default for every
// other route: the 21st request to a sibling endpoint (/health) from one IP,
// through the REAL authMiddleware -> package rateLimiter wiring, is still
// throttled with 429. The package limiter is swapped for a fresh instance so
// this test's count starts at zero regardless of what ran before it.
func TestDefaultRateLimitStillAppliesToSiblingEndpoint(t *testing.T) {
	key := useTestAPIKey(t)

	origLimiter := rateLimiter
	rateLimiter = NewRateLimiter(DefaultRateLimitRequests)
	t.Cleanup(func() { rateLimiter = origLimiter })

	handler := authMiddleware(handleHealth)

	const remoteAddr = "203.0.113.20:5555"
	const requests = 21 // one past RateLimitRequests(20)
	var lastCode int
	for i := 0; i < requests; i++ {
		req := authorizedRequest(http.MethodGet, "/health", key, remoteAddr)
		rec := httptest.NewRecorder()
		handler(rec, req)
		lastCode = rec.Code
		if i < DefaultRateLimitRequests && lastCode == http.StatusTooManyRequests {
			t.Fatalf("request %d/%d throttled early, want the first %d to pass", i+1, requests, DefaultRateLimitRequests)
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("request %d = %d, want %d -- the default per-IP budget must still apply", requests, lastCode, http.StatusTooManyRequests)
	}
}

// TestParseRateLimitEnvDefaults tests that parseRateLimitEnv returns the
// default when the environment variable is unset.
func TestParseRateLimitEnvDefaults(t *testing.T) {
	// Ensure the env var is not set
	os.Unsetenv("TEST_RATE_LIMIT")

	got := parseRateLimitEnv("TEST_RATE_LIMIT", 42)
	if got != 42 {
		t.Fatalf("parseRateLimitEnv with unset var: got %d, want 42", got)
	}
}

// TestParseRateLimitEnvValidValue tests that parseRateLimitEnv parses a
// positive integer from the environment.
func TestParseRateLimitEnvValidValue(t *testing.T) {
	t.Setenv("TEST_RATE_LIMIT", "75")

	got := parseRateLimitEnv("TEST_RATE_LIMIT", 42)
	if got != 75 {
		t.Fatalf("parseRateLimitEnv with valid value: got %d, want 75", got)
	}
}

// TestParseRateLimitEnvInvalidValue tests that parseRateLimitEnv falls back
// to the default and logs a warning for non-numeric values.
func TestParseRateLimitEnvInvalidValue(t *testing.T) {
	t.Setenv("TEST_RATE_LIMIT", "not_a_number")

	got := parseRateLimitEnv("TEST_RATE_LIMIT", 42)
	if got != 42 {
		t.Fatalf("parseRateLimitEnv with invalid value: got %d, want 42 (default)", got)
	}
}

// TestParseRateLimitEnvNonPositive tests that parseRateLimitEnv falls back
// to the default and logs a warning for non-positive values.
func TestParseRateLimitEnvNonPositive(t *testing.T) {
	t.Setenv("TEST_RATE_LIMIT", "0")

	got := parseRateLimitEnv("TEST_RATE_LIMIT", 42)
	if got != 42 {
		t.Fatalf("parseRateLimitEnv with zero value: got %d, want 42 (default)", got)
	}

	t.Setenv("TEST_RATE_LIMIT", "-10")
	got = parseRateLimitEnv("TEST_RATE_LIMIT", 42)
	if got != 42 {
		t.Fatalf("parseRateLimitEnv with negative value: got %d, want 42 (default)", got)
	}
}

func TestParseRateLimitEnvWithFallback(t *testing.T) {
	os.Unsetenv("TEST_CS_RATE_LIMIT")
	os.Unsetenv("TEST_CK_RATE_LIMIT")

	// Both unset
	if got := parseRateLimitEnvWithFallback("TEST_CS_RATE_LIMIT", "TEST_CK_RATE_LIMIT", 50); got != 50 {
		t.Fatalf("both unset: got %d, want 50", got)
	}

	// Fallback set
	t.Setenv("TEST_CK_RATE_LIMIT", "80")
	if got := parseRateLimitEnvWithFallback("TEST_CS_RATE_LIMIT", "TEST_CK_RATE_LIMIT", 50); got != 80 {
		t.Fatalf("fallback set: got %d, want 80", got)
	}

	// Primary set (takes precedence)
	t.Setenv("TEST_CS_RATE_LIMIT", "120")
	if got := parseRateLimitEnvWithFallback("TEST_CS_RATE_LIMIT", "TEST_CK_RATE_LIMIT", 50); got != 120 {
		t.Fatalf("primary set: got %d, want 120", got)
	}
}

func TestGetEnvWithFallback(t *testing.T) {
	os.Unsetenv("TEST_CS_ENV")
	os.Unsetenv("TEST_CK_ENV")

	if got := getEnvWithFallback("TEST_CS_ENV", "TEST_CK_ENV"); got != "" {
		t.Fatalf("both unset: got %q, want empty", got)
	}

	t.Setenv("TEST_CK_ENV", "fallback_val")
	if got := getEnvWithFallback("TEST_CS_ENV", "TEST_CK_ENV"); got != "fallback_val" {
		t.Fatalf("fallback set: got %q, want fallback_val", got)
	}

	t.Setenv("TEST_CS_ENV", "primary_val")
	if got := getEnvWithFallback("TEST_CS_ENV", "TEST_CK_ENV"); got != "primary_val" {
		t.Fatalf("primary set: got %q, want primary_val", got)
	}
}

// (c) Every response carries the budget state, and a throttled one carries a
// usable Retry-After. The Laravel consumer could not distinguish a 429 from a
// real outage because the server advertised nothing, so it retried blind;
// these headers are the contract that lets a client back off deliberately.
//
// The assertions are on the shape a client actually depends on: Limit is the
// configured budget, Remaining counts down and floors at 0 once refused, Reset
// is a future instant, and Retry-After is at least 1 -- never 0, which would
// invite an immediate retry guaranteed to be refused again.
func TestRateLimitHeadersAdvertiseBudget(t *testing.T) {
	key := useTestAPIKey(t)

	const budget = 3
	limiter := NewRateLimiter(budget)
	handler := authMiddlewareWithLimiter(limiter, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Exhaust the budget, checking the countdown on the way down.
	for i := 0; i < budget; i++ {
		rec := httptest.NewRecorder()
		handler(rec, authorizedRequest("GET", "/stats", key, "203.0.113.7:5000"))

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 within a budget of %d", i+1, rec.Code, budget)
		}
		if got, want := rec.Header().Get("X-RateLimit-Limit"), strconv.Itoa(budget); got != want {
			t.Errorf("request %d X-RateLimit-Limit = %q, want %q", i+1, got, want)
		}
		if got, want := rec.Header().Get("X-RateLimit-Remaining"), strconv.Itoa(budget-i-1); got != want {
			t.Errorf("request %d X-RateLimit-Remaining = %q, want %q", i+1, got, want)
		}
		reset, err := strconv.ParseInt(rec.Header().Get("X-RateLimit-Reset"), 10, 64)
		if err != nil {
			t.Fatalf("request %d X-RateLimit-Reset = %q, want a Unix timestamp: %v",
				i+1, rec.Header().Get("X-RateLimit-Reset"), err)
		}
		if reset <= time.Now().Unix()-1 {
			t.Errorf("request %d X-RateLimit-Reset = %d, want an instant in the future", i+1, reset)
		}
	}

	// One over: refused, and told when to come back.
	rec := httptest.NewRecorder()
	handler(rec, authorizedRequest("GET", "/stats", key, "203.0.113.7:5000"))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d = %d, want %d once the budget is spent",
			budget+1, rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("throttled X-RateLimit-Remaining = %q, want %q", got, "0")
	}
	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("throttled Retry-After = %q, want an integer count of seconds: %v",
			rec.Header().Get("Retry-After"), err)
	}
	if retryAfter < 1 || retryAfter > int(RateLimitWindow.Seconds())+1 {
		t.Errorf("throttled Retry-After = %d, want between 1 and %d seconds",
			retryAfter, int(RateLimitWindow.Seconds())+1)
	}

	// A cross-origin client must be able to read all of the above.
	if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "Retry-After") {
		t.Errorf("throttled Access-Control-Expose-Headers = %q, want it to expose Retry-After", got)
	}
}
