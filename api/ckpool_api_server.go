package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"cashstratumapi/internal/ckconf"
)

const (
	// Configuration defaults
	DefaultAPIKey            = "CHANGE_ME_GENERATE_WITH_OPENSSL_RAND_HEX_32"
	DefaultLogPath           = "" // Will be set to ~/ckpool/logs/ckpool.log if not provided
	DefaultUserLogPath       = "" // Will be set to ~/ckpool/logs/users if not provided
	DefaultPort              = "8888"
	DefaultStratumHost       = "127.0.0.1:3333"
	MaxLines                 = 1000
	DefaultRateLimitRequests = 20
	RateLimitWindow          = 60 * time.Second
	CacheSize                = 100
	CacheTTL                 = 60 * time.Second

	// DefaultSharesRateLimitRequests is /shares' own per-IP budget, wider than the
	// default RateLimitRequests. The default 20/60s is sized for many
	// distinct callers (miners, dashboards); /shares' intended consumer is
	// ONE Laravel proxy polling every 2-3s (~20-30 req/min) -- right at or
	// above the default ceiling. Give /shares headroom instead of loosening
	// the default for every other endpoint. Window is unchanged (still
	// RateLimitWindow) -- only the request count differs.
	DefaultSharesRateLimitRequests = 60

	// DefaultPingRateLimitRequests is /ping's own per-IP budget. /ping is
	// unauthenticated (see pingMiddleware) and sampled several times per
	// visitor page load, so it gets a wider budget than the default rather
	// than sharing a bucket sized for a handful of authenticated dashboards.
	DefaultPingRateLimitRequests = 60

	// ProbeWorkerSuffix is the worker segment /coinbase authorises its
	// phantom probe connections under ("<user>."+ProbeWorkerSuffix), so the
	// resulting entry is identifiable in the miner's own stats file and can
	// be filtered back out by /user-file (see filterProbeWorkers). One
	// exported constant so both places agree on the exact string.
	ProbeWorkerSuffix = "cashstratum-api"
)

// DefaultUpdateInterval mirrors ckpool's own default (src/ckpool.c) for how
// often it broadcasts a fresh mining.notify job on an otherwise idle pool,
// used when ckpool.conf has no update_interval key. Never hardcode a
// deployment-specific value here -- ckconf.LoadUpdateInterval reads the real
// one from ckpool.conf when it is set. Re-exported from ckconf so the rest
// of this package (stratumJobDeadline's zero-conf default, startup logging)
// keeps a single source of truth.
const DefaultUpdateInterval = ckconf.DefaultUpdateInterval

// stratumDeadlineSlack is added on top of the pool's own update_interval when
// deriving how long /coinbase waits on the stratum connection for a job --
// margin for network latency and ckpool's own processing, not a guess at the
// interval itself.
const stratumDeadlineSlack = 5 * time.Second

// coinbaseWriteDeadlineSlack extends /coinbase's per-request HTTP write
// deadline (see handleCoinbase) beyond stratumJobDeadline, so there is time
// left to actually encode and write the JSON response after the stratum wait
// concludes -- setting it equal to stratumJobDeadline would let the write
// deadline expire in the same instant the stratum read does.
const coinbaseWriteDeadlineSlack = 5 * time.Second

var (
	// Global configuration
	apiKey      string
	logPath     string
	userLogPath string
	port        string

	// Rate limit budgets, set from environment at startup; see initRateLimits.
	rateLimitRequests       = DefaultRateLimitRequests
	sharesRateLimitRequests = DefaultSharesRateLimitRequests
	pingRateLimitRequests   = DefaultPingRateLimitRequests

	// btcdRPC is the node RPC endpoint lifted from ckpool.conf at startup,
	// nil when no usable one was found. See ckconf.LoadBtcdRPC.
	btcdRPC *ckconf.BtcdEndpoint

	// stratumJobDeadline is how long /coinbase's stratum connection waits for
	// a post-authorise mining.notify before giving up, set once at startup
	// from ckpool.conf's own update_interval plus stratumDeadlineSlack (see
	// ckconf.LoadUpdateInterval). Defaults to DefaultUpdateInterval+slack
	// until main() resolves the real conf.
	stratumJobDeadline = DefaultUpdateInterval + stratumDeadlineSlack

	// Server start time for uptime calculation
	serverStartTime = time.Now()

	// Rate limiting
	rateLimiter *RateLimiter

	// /shares' own, wider budget -- see DefaultSharesRateLimitRequests for why.
	sharesRateLimiter *RateLimiter

	// /ping's own budget, enforced with no authentication -- see
	// pingMiddleware and DefaultPingRateLimitRequests for why.
	pingRateLimiter *RateLimiter

	// Response cache
	responseCache = NewLRUCache(CacheSize)

	// /coinbase request coalescing. A cache miss can hold a short-lived
	// Stratum connection open for an entire update interval, so concurrent
	// misses for the same username must share one probe rather than creating
	// duplicate phantom workers.
	coinbaseFlights = NewCoinbaseFlightGroup()

	// Metrics
	requestCounter int64
	errorCounter   int64
	metricsMutex   sync.RWMutex
)

// getEnvWithFallback returns the value of the primary environment variable if set,
// otherwise falling back to the secondary variable.
func getEnvWithFallback(primary, fallback string) string {
	if val := os.Getenv(primary); val != "" {
		return val
	}
	return os.Getenv(fallback)
}

// parseRateLimitEnv reads a rate limit budget from an environment variable,
// returning the parsed positive integer or the default when the variable is
// unset, non-numeric, or non-positive. Non-positive and parse errors trigger
// a logged warning naming both the variable and the bad value.
func parseRateLimitEnv(envVar string, defaultVal int) int {
	val := os.Getenv(envVar)
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(val)
	if err != nil || parsed <= 0 {
		log.Printf("Rate limit: %s=%q is not a positive integer, using default %d", envVar, val, defaultVal)
		return defaultVal
	}
	return parsed
}

// parseRateLimitEnvWithFallback reads a rate limit budget checking primary first,
// falling back to fallback if primary is unset or empty.
func parseRateLimitEnvWithFallback(primary, fallback string, defaultVal int) int {
	if os.Getenv(primary) != "" {
		return parseRateLimitEnv(primary, defaultVal)
	}
	return parseRateLimitEnv(fallback, defaultVal)
}

// RateLimiter tracks request rates per IP against its own maxRequests
// budget, so different routes can carry different budgets without
// duplicating the sliding-window bookkeeping (see sharesRateLimiter).
type RateLimiter struct {
	mu          sync.RWMutex
	clients     map[string]*clientInfo
	maxRequests int
}

type clientInfo struct {
	count     int
	resetTime time.Time
}

func NewRateLimiter(maxRequests int) *RateLimiter {
	rl := &RateLimiter{
		clients:     make(map[string]*clientInfo),
		maxRequests: maxRequests,
	}
	// Clean up old entries periodically
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, info := range rl.clients {
			if now.After(info.resetTime) {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Allow records one request from ip and reports whether it fits the budget.
//
// It also returns the budget state the caller needs to advertise in
// X-RateLimit-* headers: how many requests remain in the current window and
// when that window resets. Returning them from here rather than exposing the
// map is deliberate -- count and resetTime are only consistent while the lock
// is held, so any caller reading them separately would race.
//
// The window is fixed, not sliding: it opens on a client's first request and
// resets exactly RateLimitWindow later, so reset is a real wall-clock instant
// a client can wait for rather than an estimate.
func (rl *RateLimiter) Allow(ip string) (allowed bool, remaining int, reset time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	client, exists := rl.clients[ip]

	if !exists || now.After(client.resetTime) {
		reset = now.Add(RateLimitWindow)
		rl.clients[ip] = &clientInfo{
			count:     1,
			resetTime: reset,
		}
		return true, rl.maxRequests - 1, reset
	}

	if client.count >= rl.maxRequests {
		return false, 0, client.resetTime
	}

	client.count++
	return true, rl.maxRequests - client.count, client.resetTime
}

// LRUCache for response caching
type LRUCache struct {
	mu       sync.Mutex
	capacity int
	cache    map[string]*cacheEntry
	order    []string
}

type cacheEntry struct {
	data      interface{}
	timestamp time.Time
}

func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*cacheEntry),
		order:    make([]string, 0, capacity),
	}
}

func (c *LRUCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.cache[key]
	if !exists {
		return nil, false
	}

	// Check if entry has expired
	if time.Since(entry.timestamp) > CacheTTL {
		c.removeLocked(key)
		return nil, false
	}

	c.touchLocked(key)
	return entry.data, true
}

// GetPersistent returns a cache entry without applying CacheTTL. It is used
// only for explicitly candidate-scoped coinbase proofs: the caller supplies
// the candidate height that deterministically invalidates the cached value.
// Capacity eviction still applies, so this does not turn the cache into an
// unbounded store.
func (c *LRUCache) GetPersistent(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.cache[key]
	if !exists {
		return nil, false
	}

	c.touchLocked(key)
	return entry.data, true
}

func (c *LRUCache) Set(key string, data interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.capacity <= 0 {
		return
	}

	if _, exists := c.cache[key]; exists {
		c.cache[key] = &cacheEntry{data: data, timestamp: time.Now()}
		c.touchLocked(key)

		return
	}

	if len(c.cache) >= c.capacity {
		oldest := c.order[0]
		c.removeLocked(oldest)
	}

	c.cache[key] = &cacheEntry{data: data, timestamp: time.Now()}
	c.order = append(c.order, key)
}

func (c *LRUCache) touchLocked(key string) {
	for index, orderedKey := range c.order {
		if orderedKey != key {
			continue
		}

		c.order = append(c.order[:index], c.order[index+1:]...)
		break
	}
	c.order = append(c.order, key)
}

func (c *LRUCache) removeLocked(key string) {
	delete(c.cache, key)
	for index, orderedKey := range c.order {
		if orderedKey != key {
			continue
		}

		c.order = append(c.order[:index], c.order[index+1:]...)
		return
	}
}

// API Response structures - must match Python exactly
type TailResponse struct {
	Lines     []string `json:"lines"`
	Timestamp int64    `json:"timestamp"`
	Error     string   `json:"error,omitempty"`
}

type GrepResponse struct {
	Lines     []string `json:"lines"`
	Pattern   string   `json:"pattern"`
	Timestamp int64    `json:"timestamp"`
}

type FindBlockResponse struct {
	Found     bool     `json:"found"`
	Lines     []string `json:"lines"`
	Height    string   `json:"height"`
	Timestamp int64    `json:"timestamp"`
	Error     string   `json:"error,omitempty"`
}

type UserFileResponse struct {
	Username string      `json:"username"`
	Content  interface{} `json:"content"`
	Raw      bool        `json:"raw,omitempty"`
	Error    string      `json:"error,omitempty"`
}

type StatsResponse struct {
	Hashrate     *string `json:"hashrate"`
	Workers      int     `json:"workers"`
	Users        int     `json:"users"`
	Transactions int     `json:"transactions"`
	BlockHeight  *int    `json:"block_height"`
	// Node is the BCH node's own view of the chain, nil when the node could
	// not be reached (or no RPC endpoint was configured) -- in which case
	// BlockHeight falls back to the log-scrape below and may be stale.
	Node      *NodeInfo `json:"node,omitempty"`
	Timestamp int64     `json:"timestamp"`
	Error     string    `json:"error,omitempty"`
}

// NodeInfo mirrors the fields of the node's getblockchaininfo that a pool
// dashboard actually needs -- the same set the host's `bch-status` helper
// prints, so the two can be reconciled by eye during an incident.
type NodeInfo struct {
	Chain       string  `json:"chain"`
	Blocks      int     `json:"blocks"`
	Headers     int     `json:"headers"`
	ProgressPct float64 `json:"progress_pct"`
	Difficulty  float64 `json:"difficulty"`
	IBD         bool    `json:"ibd"`
	// Synced is the single boolean a caller should gate on: the node has
	// caught up to every header it knows about and is out of initial block
	// download. A node one block behind its own headers is still syncing,
	// however healthy `blocks` looks on its own.
	Synced bool `json:"synced"`
}

type UserLogResponse struct {
	Lines     []string `json:"lines"`
	Exists    bool     `json:"exists"`
	Username  string   `json:"username,omitempty"`
	Timestamp int64    `json:"timestamp"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
	LogExists bool   `json:"log_exists"`
	LogSize   int64  `json:"log_size"`
	Uptime    int64  `json:"uptime"`
	Version   string `json:"version,omitempty"`
}

type MetricsResponse struct {
	Uptime           int64 `json:"uptime"`
	RequestsTotal    int64 `json:"requests_total"`
	ErrorsTotal      int64 `json:"errors_total"`
	RateLimitEntries int   `json:"rate_limit_entries"`
	CacheEntries     int   `json:"cache_entries"`
	Timestamp        int64 `json:"timestamp"`
}

type CoinbaseOutput struct {
	Value    int64  `json:"value"`     // satoshis
	ValueBCH string `json:"value_bch"` // formatted BCH
	Address  string `json:"address"`
	Type     string `json:"type"` // "miner", "pool_fee" or "op_return"
}

type CoinbaseResponse struct {
	Username        string           `json:"username"`
	CoinbaseHex     string           `json:"coinbase_hex"`
	CoinbaseMessage string           `json:"coinbase_message"`
	Outputs         []CoinbaseOutput `json:"outputs"`
	TotalValue      int64            `json:"total_value"`     // satoshis
	TotalValueBCH   string           `json:"total_value_bch"` // formatted BCH
	BlockHeight     *int             `json:"block_height"`
	NetworkBits     string           `json:"network_bits"`
	Timestamp       int64            `json:"timestamp"`
	Error           string           `json:"error,omitempty"`
}

type coinbaseFetchResult struct {
	response CoinbaseResponse
	status   int
}

type coinbaseFlightCall struct {
	done         chan struct{}
	requestKey   string
	result       coinbaseFetchResult
	panicPayload any
}

// CoinbaseFlightGroup is a dependency-free, endpoint-specific singleflight
// group. It permits only one active probe per username. Matching candidate
// requests share that probe; different candidates wait for the slot and then
// resolve independently so they cannot receive a mismatched proof.
type CoinbaseFlightGroup struct {
	mu    sync.Mutex
	calls map[string]*coinbaseFlightCall
}

func NewCoinbaseFlightGroup() *CoinbaseFlightGroup {
	return &CoinbaseFlightGroup{calls: make(map[string]*coinbaseFlightCall)}
}

func (g *CoinbaseFlightGroup) Do(
	ctx context.Context,
	username string,
	requestKey string,
	fetch func() coinbaseFetchResult,
) (result coinbaseFetchResult, err error) {
	for {
		g.mu.Lock()
		if call, exists := g.calls[username]; exists {
			sameRequest := call.requestKey == requestKey
			g.mu.Unlock()

			select {
			case <-call.done:
				if !sameRequest {
					// A different candidate waited for the username's probe slot,
					// but must now resolve its own cache/fetch instead of receiving
					// the completed candidate's proof.
					continue
				}
				if call.panicPayload != nil {
					panic(call.panicPayload)
				}
				return call.result, nil
			case <-ctx.Done():
				return coinbaseFetchResult{}, ctx.Err()
			}
		}

		call := &coinbaseFlightCall{
			done:       make(chan struct{}),
			requestKey: requestKey,
		}
		g.calls[username] = call
		g.mu.Unlock()

		func() {
			defer func() {
				call.panicPayload = recover()

				g.mu.Lock()
				delete(g.calls, username)
				close(call.done)
				g.mu.Unlock()

				if call.panicPayload != nil {
					panic(call.panicPayload)
				}
			}()

			call.result = fetch()
		}()

		return call.result, nil
	}
}

type StratumSubscribe struct {
	ID     int           `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

type StratumResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  interface{}     `json:"error"`
}

type StratumNotify struct {
	Params []interface{} `json:"params"`
	Method string        `json:"method"`
}

// ShareRecord mirrors one line of a ckpool `.sharelog` file. Field names are
// ckpool's own (src/stratifier.c share_json()), kept verbatim rather than
// Go-cased so callers can match the on-disk log directly. RejectReason and
// Error are nullable in the log (present as JSON null, or simply absent on
// an accepted share), hence the pointer / RawMessage types.
type ShareRecord struct {
	WorkinfoID   int64           `json:"workinfoid"`
	ClientID     int64           `json:"clientid"`
	Enonce1      string          `json:"enonce1"`
	Nonce2       string          `json:"nonce2"`
	Nonce        string          `json:"nonce"`
	Ntime        string          `json:"ntime"`
	Diff         float64         `json:"diff"`
	SDiff        float64         `json:"sdiff"`
	Hash         string          `json:"hash"`
	Result       bool            `json:"result"`
	RejectReason *string         `json:"reject-reason"`
	Error        json.RawMessage `json:"error"`
	Errn         int             `json:"errn"`
	CreateDate   string          `json:"createdate"`
	CreateBy     string          `json:"createby"`
	CreateCode   string          `json:"createcode"`
	CreateInet   string          `json:"createinet"`
	Workername   string          `json:"workername"`
	// Worker is the worker segment of Workername, derived at read time --
	// the sharelog itself carries no such field, so it is always recomputed
	// and never trusted from the line. See shortWorkerName.
	Worker   string `json:"worker"`
	Username string `json:"username"`
	Address  string `json:"address"`
	Agent    string `json:"agent"`
	// VersionMask is the BIP310 version-rolling mask the miner submitted
	// (params[5] of mining.submit), rendered by ckpool as 8 hex digits and
	// "00000000" when the client sent none. Appended to the sharelog record
	// last, so it is absent from every line written before that change --
	// it decodes as "" on those, which is why it is not a pointer.
	VersionMask string `json:"version_mask"`
}

// SharesResponse is /shares' payload.
type SharesResponse struct {
	Shares []ShareRecord `json:"shares"`
	// Height is the current highest known sharelog height dir (decimal,
	// decoded from its %08x name), nil when no height dir exists yet (pool
	// has not found work since it started). A cursor'd forward read can span
	// a block rollover, so this is NOT a guarantee every returned share
	// belongs to this height -- it is "where the pool is now", matching
	// StatsResponse.BlockHeight's convention.
	Height *int `json:"height"`
	// NextCursor resumes a forward read exactly where this response left
	// off: same position (no data loss) on an empty poll, one position past
	// the last line consumed otherwise. Opaque to callers -- see
	// encodeCursor/decodeCursor.
	NextCursor string `json:"next_cursor,omitempty"`
	Timestamp  int64  `json:"timestamp"`
	Error      string `json:"error,omitempty"`
}

// lastLines returns at most the final n lines of command output, replacing the
// `| tail -n` that the handlers used to get from a shell. Always returns a
// non-nil slice so the JSON encodes as [] rather than null.
func lastLines(output []byte, n int) []string {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return []string{}
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// Middleware for authentication and rate limiting, against the default
// per-IP budget (rateLimiter). Routes needing a different budget call
// authMiddlewareWithLimiter directly instead (see /shares).
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return authMiddlewareWithLimiter(rateLimiter, next)
}

// authMiddlewareWithLimiter is authMiddleware parameterized over which
// RateLimiter enforces the per-IP budget, so a route can opt into a
// different budget without duplicating the auth/CORS/counter logic.
func authMiddlewareWithLimiter(limiter *RateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check Authorization header
		auth := r.Header.Get("Authorization")
		expectedAuth := "Bearer " + apiKey

		// Constant-time: a plain != returns on the first differing byte, so
		// response latency leaks a prefix oracle over the key. ConstantTimeCompare
		// returns 0 on a length mismatch without comparing, which is fine -- the
		// key's length is not the secret.
		if subtle.ConstantTimeCompare([]byte(auth), []byte(expectedAuth)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			incrementErrorCounter()
			return
		}

		clientIP := getClientIP(r)
		if !applyRateLimit(w, limiter, clientIP) {
			return
		}

		w.Header().Set("Content-Type", "application/json")

		incrementRequestCounter()
		next(w, r)
	}
}

// applyRateLimit enforces limiter's per-IP budget for clientIP and
// advertises it via the X-RateLimit-* / CORS headers, the same way
// authMiddlewareWithLimiter always has -- factored out so pingMiddleware
// (unauthenticated, its own limiter) gets identical throttling behaviour
// without duplicating it.
//
// The budget state is advertised on EVERY response, not just the 429 -- a
// client that can see itself approaching the limit can slow down instead of
// discovering it by being refused. CORS is set before the throttle check,
// not after it: a browser client that gets a 429 must still be allowed to
// read the headers that tell it when to come back, and Expose-Headers is
// what makes the X-RateLimit-* set readable cross-origin at all.
//
// It returns false once it has already written a 429 response, at which
// point the caller must not write anything further.
func applyRateLimit(w http.ResponseWriter, limiter *RateLimiter, clientIP string) bool {
	allowed, remaining, reset := limiter.Allow(clientIP)

	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limiter.maxRequests))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers",
		"X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Retry-After")

	if !allowed {
		// Round up and floor at 1: a sub-second remainder truncates to
		// 0, and "Retry-After: 0" invites an immediate retry that is
		// guaranteed to be refused again.
		retryAfter := int(time.Until(reset).Seconds()) + 1
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		incrementErrorCounter()
		return false
	}

	return true
}

func getClientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// pingClientIP derives the caller's IP for /ping's rate limiter: ordinarily
// RemoteAddr, but when RemoteAddr is loopback -- the shape of a request
// arriving through a local reverse proxy such as Caddy -- the FIRST entry of
// X-Forwarded-For is used instead. Without this, every browser behind the proxy
// would share one 127.0.0.1 bucket. XFF is trusted ONLY in the loopback case:
// honouring it from a non-loopback RemoteAddr would let any direct caller spoof
// its own rate-limit bucket.
//
// Loopback detection is net.ParseIP(host).IsLoopback() after
// net.SplitHostPort, not a string prefix check -- that also correctly
// covers ::1 and other loopback forms a prefix match would miss.
func pingClientIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
			if first != "" {
				return first
			}
		}
	}

	return host
}

// pingMiddleware enforces /ping's own rate limit (applyRateLimit, same
// headers and 429 shape as every authenticated route) with deliberately NO
// authentication -- see handlePing for why this route is the one open hole in
// an otherwise fail-closed API, and why that is safe: it carries no data and
// costs nothing to serve.
func pingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := pingClientIP(r)
		if !applyRateLimit(w, pingRateLimiter, clientIP) {
			return
		}

		next(w, r)
	}
}

// Handler functions
func handleTail(w http.ResponseWriter, r *http.Request) {
	linesParam := r.URL.Query().Get("lines")
	numLines := 100
	if linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			numLines = min(n, MaxLines)
		}
	}

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		json.NewEncoder(w).Encode(TailResponse{
			Lines:     []string{},
			Error:     "Log file not found",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Execute tail command
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tail", "-n", strconv.Itoa(numLines), logPath)
	output, err := cmd.Output()

	var lines []string
	if err == nil && len(output) > 0 {
		lines = strings.Split(strings.TrimSpace(string(output)), "\n")
	} else {
		lines = []string{}
	}

	json.NewEncoder(w).Encode(TailResponse{
		Lines:     lines,
		Timestamp: time.Now().Unix(),
	})
}

func handleGrep(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("pattern")
	if pattern == "" {
		http.Error(w, "Pattern required", http.StatusBadRequest)
		return
	}

	// Check cache
	cacheKey := "grep:" + pattern
	if cached, found := responseCache.Get(cacheKey); found {
		json.NewEncoder(w).Encode(cached)
		return
	}

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		response := GrepResponse{
			Lines:     []string{},
			Pattern:   pattern,
			Timestamp: time.Now().Unix(),
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// QuoteMeta escapes REGEX metacharacters, not SHELL ones -- a single quote
	// is not a regex metacharacter and passed through untouched. The result used
	// to be interpolated into an `sh -c` string wrapped in single quotes, so
	//     ?pattern=foo' ; id ; echo '
	// closed the quoting and ran arbitrary commands as the pool's own user.
	//
	// There is no shell here any more. grep receives the pattern as a single
	// argv element via -e, so there is nothing to quote and nothing to break out
	// of. QuoteMeta stays because this endpoint has always matched literally,
	// and `--` stops a log path that begins with a dash being read as a flag.
	escapedPattern := regexp.QuoteMeta(pattern)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "grep", "-a", "-E", "-e", escapedPattern, "--", logPath)
	output, _ := cmd.Output()

	// Replaces the old `| tail -500`, which only existed because of the shell.
	lines := lastLines(output, 500)

	response := GrepResponse{
		Lines:     lines,
		Pattern:   pattern,
		Timestamp: time.Now().Unix(),
	}

	// Cache the response
	responseCache.Set(cacheKey, response)

	json.NewEncoder(w).Encode(response)
}

func handleFindBlock(w http.ResponseWriter, r *http.Request) {
	height := r.URL.Query().Get("height")
	if height == "" {
		http.Error(w, "Block height required", http.StatusBadRequest)
		return
	}

	// Validate height is numeric
	if _, err := strconv.Atoi(height); err != nil {
		http.Error(w, "Block height required", http.StatusBadRequest)
		return
	}

	// Check cache
	cacheKey := "block:" + height
	if cached, found := responseCache.Get(cacheKey); found {
		json.NewEncoder(w).Encode(cached)
		return
	}

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		response := FindBlockResponse{
			Found:     false,
			Lines:     []string{},
			Height:    height,
			Error:     "Log file not found",
			Timestamp: time.Now().Unix(),
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// Search patterns
	patterns := []string{
		fmt.Sprintf("Solved and confirmed block %s", height),
		fmt.Sprintf("BLOCK ACCEPTED.*height %s", height),
		fmt.Sprintf("Found block.*%s", height),
	}

	// height is validated numeric above, so this was not exploitable the way
	// /grep was. It is still the same shape -- a caller-derived value
	// interpolated into a shell string -- and one future edit relaxing that
	// validation would make it exploitable silently. No shell here either.
	var allMatches []string
	for _, pattern := range patterns {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "grep", "-a", "-C", "5", "-e", pattern, "--", logPath)
		output, _ := cmd.Output()
		cancel()

		allMatches = append(allMatches, lastLines(output, 50)...)
	}

	response := FindBlockResponse{
		Found:     len(allMatches) > 0,
		Lines:     allMatches,
		Height:    height,
		Timestamp: time.Now().Unix(),
	}

	// Cache found blocks forever
	if response.Found {
		responseCache.Set(cacheKey, response)
	}

	json.NewEncoder(w).Encode(response)
}

// cashaddrPrefixes are the CashAddr forms a stratum username may carry. The
// pool names each user's stats file after the exact auth string, so the same
// key can live under a prefixed or a bare filename depending on how the miner
// logged in.
var cashaddrPrefixes = []string{"bitcoincash:", "bchtest:", "bchreg:"}

// cashaddrCharset is the base32 alphabet CashAddr payloads are encoded in
// (the CashAddr spec; ckpool's own copy lives at src/stratifier.c:5438).
// Used only to recognise a *bare* CashAddr string by shape -- a bare address
// carries no prefix, so length + alphabet is the only signal available.
const cashaddrCharset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// cashaddrPrefix is this pool's own CashAddr network prefix (no trailing
// ":"), used by canonicalAuthUser to prepend the colon-qualified form to a
// bare address. Set once in main from CKPOOL_CASHADDR_PREFIX; defaults to
// "bitcoincash" (production is mainnet). A beast regtest/testnet4 rig sets
// it to "bchreg"/"bchtest" instead.
var cashaddrPrefix = "bitcoincash"

// looksLikeCashAddr reports whether token is CashAddr-shaped: it carries one
// of cashaddrPrefixes case-insensitively, or -- for a bare address, which
// carries no marker at all -- is exactly 42 characters drawn from
// cashaddrCharset with no ":". A legacy Base58 address never matches either
// arm (it has no prefix and is not 42 chars of this alphabet), which is
// deliberate: it is case-significant and must pass through untouched.
func looksLikeCashAddr(token string) bool {
	lower := strings.ToLower(token)
	for _, p := range cashaddrPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	if strings.Contains(token, ":") || len(token) != 42 {
		return false
	}
	for _, c := range lower {
		if !strings.ContainsRune(cashaddrCharset, c) {
			return false
		}
	}
	return true
}

// canonicalAuthUser derives the exact identity string a caller should
// authorise under: the same first "._"-split token ckpool itself derives at
// authorise time (strsep(&base_username, "._"), src/stratifier.c:5466,
// see shortWorkerName above), canonicalised the same way ckpool's own
// normalisation block does for CashAddr identity normalization.
// A CashAddr token is lowercased and, when bare, prefixed with
// cashaddrPrefix + ":", so this probe mirrors ckpool's own canonicalisation
// and never provokes a shadow user even against an older, unpatched pool
// build. A legacy Base58 address or a plain non-address username is
// returned exactly as given -- byte-for-byte, case included.
func canonicalAuthUser(username string) string {
	token := username
	if i := strings.IndexAny(token, "._"); i >= 0 {
		token = token[:i]
	}
	if !looksLikeCashAddr(token) {
		return token
	}
	token = strings.ToLower(token)
	for _, p := range cashaddrPrefixes {
		if strings.HasPrefix(token, p) {
			return token
		}
	}
	return cashaddrPrefix + ":" + token
}

// cashaddrSpellings returns every username spelling ckpool accounting treats
// as equivalent to query: the exact string first, plus the bare form when
// query carries a known CashAddr prefix, or each known prefixed form when
// query is bare. This is the one place that owns "what counts as the same
// CashAddr identity" -- resolveUserFile (file lookup) and the /shares user
// filter (record matching) both build on it instead of re-deriving the
// stripping/prefixing logic themselves.
//
// A legacy base58 address is deliberately never folded into this set: it is
// a different string than any CashAddr spelling even when it happens to
// encode the same key, and ckpool itself keeps the two accounting identities
// separate, so matching across them here would silently merge shares that
// belong to distinct usernames on disk.
func cashaddrSpellings(query string) []string {
	spellings := []string{query}
	for _, p := range cashaddrPrefixes {
		if strings.HasPrefix(query, p) {
			return append(spellings, strings.TrimPrefix(query, p))
		}
	}
	for _, p := range cashaddrPrefixes {
		spellings = append(spellings, p+query)
	}
	return spellings
}

// resolveUserFile maps a queried username to an existing stats file: the exact
// (sanitized) name always wins; only when it is absent is the alternate
// CashAddr form tried — bare for a prefixed query, each known prefix for a
// bare one. Every candidate is filepath.Base-sanitized and confined to
// userLogPath, so a crafted name cannot escape the users dir.
func resolveUserFile(username string) (path string, resolvedName string, found bool) {
	candidates := cashaddrSpellings(username)

	absUserLogPath, _ := filepath.Abs(userLogPath)
	for _, name := range candidates {
		name = filepath.Base(name)
		candidate := filepath.Join(userLogPath, name)
		absPath, _ := filepath.Abs(candidate)
		if !strings.HasPrefix(absPath, absUserLogPath) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, name, true
		}
	}
	return "", filepath.Base(username), false
}

// shortWorkerName reduces a ckpool workername to just its worker segment --
// the part the miner actually typed after their payout address.
//
// ckpool authorises on the raw stratum username and stores it verbatim, so a
// workername is always "<payout identity><sep><worker>", with sep the FIRST
// '.' or '_': authorise() derives the username via strsep(&s, "._"), which is
// also why a genuine username can never contain either character (see the
// read_userstats() guard in src/stratifier.c). Everything past that first
// separator is the worker segment, dots included -- "addr.rig.1" -> "rig.1".
//
// username is a hint, not a requirement: the same key can be authorised in
// prefixed or bare CashAddr form, so a mismatch falls through to the
// separator scan rather than giving up.
//
// A miner who authorised with a bare address has no worker segment at all.
// That yields "" rather than echoing the address back, so a caller can render
// the single unnamed rig without having to string-match the identity again.
func shortWorkerName(workername, username string) string {
	if username != "" && strings.HasPrefix(workername, username) {
		if rest := workername[len(username):]; rest != "" && (rest[0] == '.' || rest[0] == '_') {
			return rest[1:]
		}
	}
	if i := strings.IndexAny(workername, "._"); i >= 0 {
		return workername[i+1:]
	}
	return ""
}

// annotateUserFileWorkers adds a "worker" key beside each entry's
// "workername" in a parsed user stats file, carrying just the worker segment
// (see shortWorkerName).
//
// Additive on purpose: "workername" is what ckpool wrote and what existing
// consumers match on, so it is left byte-identical. Anything that does not
// match the expected shape is passed through untouched -- the file's schema
// is ckpool's to define, and /user-file's contract is to relay it.
func annotateUserFileWorkers(content interface{}, username string) {
	obj, ok := content.(map[string]interface{})
	if !ok {
		return
	}
	arr, ok := obj["worker"].([]interface{})
	if !ok {
		return
	}
	for _, entry := range arr {
		w, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		name, ok := w["workername"].(string)
		if !ok {
			continue
		}
		w["worker"] = shortWorkerName(name, username)
	}
}

// filterProbeWorkers drops any entries from a parsed user stats file's
// "worker" array whose workername was authorised by /coinbase's own probe
// connection (workername ending in "."+ProbeWorkerSuffix or legacy ".ckpool-api" --
// see handleCoinbase), and subtracts EXACTLY that many entries from the file's
// own "workers" count, floored at 0. The count is never recomputed from the
// array's new length: ckpool's "workers" is its own independent decay
// bookkeeping and is routinely 0 while a worker is still listed, so only
// what was actually filtered is ever subtracted. Every other field passes
// through byte-for-byte untouched, and anything that does not match the
// expected shape is left alone rather than guessed at.
func filterProbeWorkers(content interface{}) {
	obj, ok := content.(map[string]interface{})
	if !ok {
		return
	}
	arr, ok := obj["worker"].([]interface{})
	if !ok {
		return
	}

	suffix := "." + ProbeWorkerSuffix
	legacySuffix := "." + "ckpool-api"
	kept := arr[:0]
	dropped := 0
	for _, entry := range arr {
		if w, ok := entry.(map[string]interface{}); ok {
			if name, ok := w["workername"].(string); ok && (strings.HasSuffix(name, suffix) || strings.HasSuffix(name, legacySuffix)) {
				dropped++
				continue
			}
		}
		kept = append(kept, entry)
	}
	if dropped == 0 {
		return
	}
	obj["worker"] = kept

	if workers, ok := obj["workers"].(float64); ok {
		newCount := workers - float64(dropped)
		if newCount < 0 {
			newCount = 0
		}
		obj["workers"] = newCount
	}
}

func handleUserFile(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("user")
	if username == "" {
		http.Error(w, "Username required", http.StatusBadRequest)
		return
	}

	// Was hardcoded to /home/elo/ckpool/logs/users, which 404s on any host where
	// the pool does not run as `elo`. handleUserLog already derives this from
	// CKPOOL_USER_LOGS_PATH; this handler was the one that got missed.
	userFile, resolvedName, found := resolveUserFile(username)
	if !found {
		json.NewEncoder(w).Encode(UserFileResponse{
			Error:    "User not found",
			Username: resolvedName,
		})
		return
	}
	// Report the name the stats actually live under, so callers learn the
	// canonical identity when a fallback form resolved.
	username = resolvedName

	info, err := os.Stat(userFile)
	if err != nil {
		json.NewEncoder(w).Encode(UserFileResponse{
			Error:    "User not found",
			Username: username,
		})
		return
	}

	// Check file size (1MB limit)
	if info.Size() > 1024*1024 {
		json.NewEncoder(w).Encode(UserFileResponse{
			Error:    "File too large",
			Username: username,
		})
		return
	}

	// Read file
	data, err := os.ReadFile(userFile)
	if err != nil {
		json.NewEncoder(w).Encode(UserFileResponse{
			Error:    err.Error(),
			Username: username,
		})
		return
	}

	// Try to parse as JSON
	var jsonContent interface{}
	if err := json.Unmarshal(data, &jsonContent); err == nil {
		filterProbeWorkers(jsonContent)
		annotateUserFileWorkers(jsonContent, username)
		json.NewEncoder(w).Encode(UserFileResponse{
			Username: username,
			Content:  jsonContent,
		})
	} else {
		// Return as raw string if not JSON
		json.NewEncoder(w).Encode(UserFileResponse{
			Username: username,
			Content:  string(data),
			Raw:      true,
		})
	}
}

// blockchainInfo is getblockchaininfo's result, trimmed to what NodeInfo needs.
type blockchainInfo struct {
	Chain                string  `json:"chain"`
	Blocks               int     `json:"blocks"`
	Headers              int     `json:"headers"`
	VerificationProgress float64 `json:"verificationprogress"`
	InitialBlockDownload bool    `json:"initialblockdownload"`
	Difficulty           float64 `json:"difficulty"`
}

// nodeRPCTimeout bounds a getblockchaininfo call. /stats is polled by a
// dashboard, so a node that is wedged, reindexing, or mid-restart must make
// the endpoint slightly less informative -- never slow. The server's own
// WriteTimeout is 10s; this stays well inside it.
const nodeRPCTimeout = 3 * time.Second

// fetchNodeInfo asks the node where the chain actually is.
//
// This replaces scraping `height[:=](\d+)` out of the tail of ckpool.log,
// which reported whatever height happened to appear in the last 200 lines --
// stale on a quiet pool, and silently absent whenever the log format shifted.
func fetchNodeInfo(ctx context.Context, ep *ckconf.BtcdEndpoint) (*NodeInfo, error) {
	if ep == nil {
		return nil, fmt.Errorf("no node RPC endpoint configured")
	}

	// ckpool.conf writes a bare host:port ("127.0.0.1:8332"); url.Parse would
	// read that as scheme "127.0.0.1", so prepend the scheme ourselves rather
	// than trusting the operator to have included one.
	endpoint := ep.URL
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}

	body := []byte(`{"jsonrpc":"1.0","id":"ckpool-api","method":"getblockchaininfo","params":[]}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(ep.Auth, ep.Pass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Bound the read: this is a trusted local node, but an unbounded
	// io.ReadAll into a 128M-capped process is not a risk worth taking.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// Never echo raw back: a 401 body is harmless, but this path must
		// not become a way to surface anything the node said about auth.
		return nil, fmt.Errorf("node RPC returned HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		Result *blockchainInfo `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("node RPC returned no result")
	}

	info := envelope.Result
	return &NodeInfo{
		Chain:       info.Chain,
		Blocks:      info.Blocks,
		Headers:     info.Headers,
		ProgressPct: math.Floor(info.VerificationProgress*100*10000) / 10000,
		Difficulty:  info.Difficulty,
		IBD:         info.InitialBlockDownload,
		Synced:      !info.InitialBlockDownload && info.Headers > 0 && info.Blocks >= info.Headers,
	}, nil
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	// Check cache
	cacheKey := "stats"
	if cached, found := responseCache.Get(cacheKey); found {
		if cachedStats, ok := cached.(StatsResponse); ok {
			if time.Now().Unix()-cachedStats.Timestamp < 30 {
				json.NewEncoder(w).Encode(cached)
				return
			}
		}
	}

	// Initialize response with nulls (matching Python)
	response := StatsResponse{
		Hashrate:     nil,
		Workers:      0,
		Users:        0,
		Transactions: 0,
		BlockHeight:  nil,
		Timestamp:    time.Now().Unix(),
	}

	// Ask the node where the chain is before touching the log. The node is
	// the authority on height; the log scrape further down only ever fills
	// BlockHeight when it is still nil, so this silently takes precedence
	// when the node answers and cleanly yields to the old path when it does
	// not. A node failure is a degraded field, never a failed request.
	nodeCtx, nodeCancel := context.WithTimeout(r.Context(), nodeRPCTimeout)
	nodeInfo, nodeErr := fetchNodeInfo(nodeCtx, btcdRPC)
	nodeCancel()
	if nodeErr == nil {
		height := nodeInfo.Blocks
		response.Node = nodeInfo
		response.BlockHeight = &height
	} else if btcdRPC != nil {
		log.Printf("Node RPC getblockchaininfo failed, using log-scraped height: %v", nodeErr)
	}

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		response.Error = "Log file not found"
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get last 200 lines
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tail", "-n", "200", logPath)
	output, err := cmd.Output()

	if err == nil && len(output) > 0 {
		lines := strings.Split(string(output), "\n")

		// Parse lines in reverse order for most recent data
		for i := len(lines) - 1; i >= 0; i-- {
			line := lines[i]

			// Pool hashrate.
			//
			// ckpool writes an SI-suffixed string: "hashrate1m": "50.8P".
			// The old pattern was ([0-9.E+]+), which matched "50.8" and threw
			// the P away -- reporting 50.8 H/s for 50.8 PH/s, off by 10^15.
			// Capture the whole quoted value and let the consumer scale it.
			if response.Hashrate == nil && strings.Contains(line, `Pool:{"hashrate1m"`) {
				re := regexp.MustCompile(`"hashrate1m":\s*"([^"]+)"`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					hashrate := matches[1]
					response.Hashrate = &hashrate
				}
			}

			// User and worker counts.
			//
			// ckpool capitalises these on the pool summary line:
			//     Pool:{"runtime": 60, ..., "Users": 1, "Workers": 1, ...}
			// The old code matched lowercase `"workers":`, which occurs only on
			// the PER-USER line, so Workers was read from whichever user
			// happened to appear in the tail and Users -- having no lowercase
			// spelling anywhere -- stayed 0 forever even with miners connected.
			if response.Workers == 0 && strings.Contains(line, `Pool:{"runtime"`) {
				if m := regexp.MustCompile(`"Workers":\s*(\d+)`).FindStringSubmatch(line); len(m) > 1 {
					if workers, err := strconv.Atoi(m[1]); err == nil {
						response.Workers = workers
					}
				}
				if m := regexp.MustCompile(`"Users":\s*(\d+)`).FindStringSubmatch(line); len(m) > 1 {
					if users, err := strconv.Atoi(m[1]); err == nil {
						response.Users = users
					}
				}
			}

			// Transaction count
			if response.Transactions == 0 && strings.Contains(line, "Stored local workbase") {
				re := regexp.MustCompile(`with (\d+) transactions?`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					if txns, err := strconv.Atoi(matches[1]); err == nil {
						response.Transactions = txns
					}
				}
			}

			// Block height
			if response.BlockHeight == nil && strings.Contains(strings.ToLower(line), "height") {
				re := regexp.MustCompile(`height[:=]\s*(\d+)`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					if height, err := strconv.Atoi(matches[1]); err == nil {
						response.BlockHeight = &height
					}
				}
			}
		}
	}

	// Cache the response
	responseCache.Set(cacheKey, response)

	json.NewEncoder(w).Encode(response)
}

func handleUserLog(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("user")
	if username == "" {
		http.Error(w, "Username required", http.StatusBadRequest)
		return
	}

	linesParam := r.URL.Query().Get("lines")
	numLines := 100
	if linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			numLines = min(n, MaxLines)
		}
	}

	// Sanitization and dir containment live inside resolveUserFile; a
	// traversal attempt now reads as not-found rather than 403.
	userLog, resolvedName, found := resolveUserFile(username)
	if !found {
		json.NewEncoder(w).Encode(UserLogResponse{
			Lines:  []string{},
			Exists: false,
		})
		return
	}
	username = resolvedName

	// Execute tail command
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tail", "-n", strconv.Itoa(numLines), userLog)
	output, err := cmd.Output()

	var logLines []string
	if err == nil && len(output) > 0 {
		logLines = strings.Split(strings.TrimSpace(string(output)), "\n")
	} else {
		logLines = []string{}
	}

	json.NewEncoder(w).Encode(UserLogResponse{
		Lines:     logLines,
		Exists:    true,
		Username:  username,
		Timestamp: time.Now().Unix(),
	})
}

// heightDirPattern matches ckpool's per-block sharelog directory naming:
// %08x of the block height, lowercase hex, exactly 8 digits
// (src/stratifier.c:1104). Anything else under the sharelog base dir is
// ignored rather than misread as a height.
var heightDirPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)

// listHeightDirs returns every valid sharelog height directory under base
// whose name is >= minDir, ascending; pass "" for no floor. Height dir names
// are fixed-width 8-hex-digit strings, so plain lexicographic comparison and
// sort are numeric comparison and sort too -- no need to parse each one just
// to compare it. Resolved fresh on every call by the caller -- never cached
// -- because ckpool starts a new height dir on every new block and a cached
// listing would silently keep serving shares from a stale one.
//
// minDir is filtered in during the same pass that reads entries, BEFORE
// sort.Strings runs -- ckpool's pruning (clean-old-blocks.sh) can leave many
// old height dirs sitting on disk for a while after mining has moved past
// them, and a cursor'd poller only ever needs dirs at or above its own
// resume height. Filtering first keeps the sort, and every request after the
// first, priced by the size of that backlog rather than by however much
// total retention happens to be sitting on disk.
func listHeightDirs(base string, minDir string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && heightDirPattern.MatchString(e.Name()) && (minDir == "" || e.Name() >= minDir) {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

// listSharelogFiles returns the *.sharelog files directly inside dir,
// ascending. Filenames are the 16-hex-digit workinfoid, zero-padded and
// monotonically increasing, so lexicographic order is chronological order.
func listSharelogFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sharelog") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files
}

// shareCursor identifies a resume point in the sharelog stream: which height
// dir, which sharelog file within it, and the byte offset into that file
// just past the last line the caller has already seen. Anchoring on
// (height dir, file) rather than a bare running offset is what survives a
// block rollover -- an offset alone would be meaningless once the pool
// moves to a new height dir with its own, unrelated files.
type shareCursor struct {
	dir    string
	file   string
	offset int64
}

// encodeCursor renders a shareCursor as the opaque string callers pass back
// via `since`. base64 just keeps the pipe-delimited internal shape out of
// callers' hands; it is not a security boundary -- decodeCursor validates
// the height-dir shape again on the way back in regardless.
func encodeCursor(c shareCursor) string {
	raw := fmt.Sprintf("%s|%s|%d", c.dir, c.file, c.offset)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a `since` value produced by encodeCursor. Any failure
// -- bad base64, wrong shape, a dir name that isn't 8 hex digits, a negative
// offset -- reports ok=false so the caller can fall back to a bootstrap read
// instead of failing the request over a corrupt or hand-edited cursor.
//
// An EMPTY file part is valid and must stay that way: it means "the start of
// this height dir", which is exactly what a bootstrap read of an idle pool
// produces -- ckpool has created the height dir for the new workbase but has
// not written a share into it yet, so there is no file to anchor on and
// encodeCursor emits "<dir>||0". Rejecting that made the server refuse its
// own output: every idle poll silently downgraded to a bootstrap tail read,
// and the first poll after mining resumed returned only the LAST `limit`
// records of the dir, dropping every share before them. resolveCursorStart
// already handles the empty file correctly (os.Stat lands on the dir).
func decodeCursor(s string) (shareCursor, bool) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return shareCursor{}, false
	}
	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 || !heightDirPattern.MatchString(parts[0]) {
		return shareCursor{}, false
	}
	offset, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || offset < 0 {
		return shareCursor{}, false
	}
	return shareCursor{dir: parts[0], file: parts[1], offset: offset}, true
}

// resolveCursorStart maps a decoded cursor to a start position guaranteed to
// exist. ckpool's own cleanup (clean-old-blocks.sh) prunes old height dirs,
// so a cursor issued hours ago may name a directory or file that is simply
// gone -- that must never error. When the cursor's own file is gone, resume
// from the start of the oldest still-existing height dir AT OR ABOVE the
// cursor's height (never below it, or already-cleaned shares would look
// like a request to replay them that can never be satisfied).
func resolveCursorStart(sharelogBase string, dirs []string, cur shareCursor) (dir, file string, offset int64) {
	if _, err := os.Stat(filepath.Join(sharelogBase, cur.dir, cur.file)); err == nil {
		return cur.dir, cur.file, cur.offset
	}
	for _, d := range dirs {
		if d >= cur.dir {
			return d, "", 0
		}
	}
	if len(dirs) > 0 {
		return dirs[len(dirs)-1], "", 0
	}
	return "", "", 0
}

// readShareLines reads complete (newline-terminated) lines from the file at
// path starting at offset, calling onLine with each raw line and the byte
// offset just past it. It stops when onLine returns false (caller has what
// it needs), the context deadline fires, or the file is exhausted, and
// returns the offset to resume from next time -- always at a line boundary.
//
// ckpool appends to sharelog files with O_APPEND, so the last bytes of a
// file being actively written may be a line with no trailing newline yet. A
// bufio.Scanner would still hand that back as a final token, silently
// consuming a share record that is still mid-write. ReadBytes distinguishes
// the two cases via io.EOF: reached with a non-empty, undelimited remainder
// means "torn write in progress" -- that remainder, and the offset, are left
// untouched so the next poll re-reads it whole once ckpool finishes.
func readShareLines(ctx context.Context, path string, offset int64, onLine func(line []byte) bool) int64 {
	f, err := os.Open(path)
	if err != nil {
		return offset
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset
	}

	reader := bufio.NewReader(f)
	pos := offset
	for {
		if ctx.Err() != nil {
			return pos
		}
		chunk, err := reader.ReadBytes('\n')
		if err != nil {
			// io.EOF (with or without a trailing partial chunk) -- nothing
			// more, or a torn final line. Either way, stop here unconsumed.
			return pos
		}
		pos += int64(len(chunk))
		line := bytes.TrimRight(chunk, "\r\n")
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if !onLine(line) {
			return pos
		}
	}
}

// appendBounded keeps at most limit records, dropping the oldest as new ones
// arrive, so the most recent shares survive when more than limit were found
// -- never the first ones seen. Used only for the bootstrap (no `since`)
// tail read; a cursor'd forward read must never drop records this way, so it
// stops at limit instead (see scanShares).
func appendBounded(records []ShareRecord, rec ShareRecord, limit int) []ShareRecord {
	if len(records) < limit {
		return append(records, rec)
	}
	copy(records, records[1:])
	records[len(records)-1] = rec
	return records
}

// scanShares walks the sharelog tree forward from (startDir, startFile,
// startOffset): the rest of startFile (or all of startDir's files, oldest
// first, when startFile is ""), then later files in startDir, then later
// height dirs from dirs in ascending order. It stops once limit matching
// records have been collected (forward mode only -- see tailMode below), the
// context deadline fires, or the tree is exhausted, and returns exactly that
// stopping point as the resume cursor -- so the next call with that cursor
// is gap-free and duplicate-free.
//
// tailMode (bootstrap, no caller cursor) scans startDir to completion and
// keeps only the MOST RECENT limit matches via appendBounded, dropping
// earlier ones as later ones arrive -- a first-time caller sees "now", not
// the start of the block. A forward read (tailMode false) instead STOPS the
// instant limit matching records are collected, because dropping any of
// them to make room for later ones would be silent data loss for a caller
// that is relying on the cursor to see every share exactly once.
// userFilter, when non-nil, is the set of CashAddr spellings (see
// cashaddrSpellings) a record's Username must belong to -- membership, not
// equality, so a sharelog line written under either spelling of the queried
// address matches regardless of which one the caller typed. nil means "no
// filter".
func scanShares(ctx context.Context, sharelogBase string, dirs []string, startDir, startFile string, startOffset int64, userFilter map[string]struct{}, limit int, tailMode bool) ([]ShareRecord, shareCursor) {
	records := []ShareRecord{}
	cursor := shareCursor{dir: startDir, file: startFile, offset: startOffset}
	if startDir == "" {
		return records, cursor
	}

	dirIdx := 0
	for i, d := range dirs {
		if d == startDir {
			dirIdx = i
			break
		}
	}

outer:
	for di := dirIdx; di < len(dirs); di++ {
		if ctx.Err() != nil {
			break
		}
		dirName := dirs[di]
		heightDir := filepath.Join(sharelogBase, dirName)
		files := listSharelogFiles(heightDir)

		fileIdx := 0
		var firstFileOffset int64
		if di == dirIdx && startFile != "" {
			for i, f := range files {
				if f == startFile {
					fileIdx = i
					firstFileOffset = startOffset
					break
				}
			}
		}

		for fi := fileIdx; fi < len(files); fi++ {
			if ctx.Err() != nil {
				break outer
			}
			name := files[fi]
			var off int64
			if di == dirIdx && fi == fileIdx {
				off = firstFileOffset
			}

			limitHit := false
			path := filepath.Join(heightDir, name)
			endOffset := readShareLines(ctx, path, off, func(line []byte) bool {
				var rec ShareRecord
				if err := json.Unmarshal(line, &rec); err != nil {
					return true // malformed but newline-terminated -- skip AND advance past
				}
				rec.Worker = shortWorkerName(rec.Workername, rec.Username)
				if userFilter != nil {
					if _, ok := userFilter[rec.Username]; !ok {
						return true
					}
				}
				if tailMode {
					records = appendBounded(records, rec, limit)
					return true
				}
				records = append(records, rec)
				if len(records) >= limit {
					limitHit = true
					return false
				}
				return true
			})

			cursor = shareCursor{dir: dirName, file: name, offset: endOffset}
			if limitHit {
				break outer
			}
		}
	}

	return records, cursor
}

func handleShares(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Sanitize BEFORE any use, same discipline as resolveUserFile -- this
	// value only ever gets compared against a record field, never used as a
	// path, but filepath.Base keeps that guarantee even if that changes later.
	userFilter := filepath.Base(q.Get("user"))
	if q.Get("user") == "" {
		userFilter = ""
	}

	limit := MaxLines
	if lp := q.Get("limit"); lp != "" {
		if n, err := strconv.Atoi(lp); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > MaxLines {
		limit = MaxLines
	}

	sinceParam := q.Get("since")

	// A cursor'd poller hits this on a tight interval (seconds, not the
	// minute-scale polling /stats and /coinbase expect); sharing their 60s
	// cache TTL would blind it to new shares for up to a minute. Freshness
	// window here is deliberately short, matched to that use, not the
	// cache's own TTL -- same embedded-timestamp pattern those two use.
	cacheKey := fmt.Sprintf("shares:%s:%s:%d", sinceParam, userFilter, limit)
	if cached, found := responseCache.Get(cacheKey); found {
		if cachedResp, ok := cached.(SharesResponse); ok {
			if time.Now().Unix()-cachedResp.Timestamp < 2 {
				json.NewEncoder(w).Encode(cached)
				return
			}
		}
	}

	response := SharesResponse{
		Shares:    []ShareRecord{},
		Timestamp: time.Now().Unix(),
	}

	// Decoded before listing the sharelog tree so the listing itself can be
	// bounded to the cursor's own height -- a cursor'd poller only ever
	// needs dirs at or above where it left off, not every height dir
	// ckpool's pruning happens to still have on disk.
	var sinceCursor shareCursor
	haveCursor := false
	if sinceParam != "" {
		if cur, ok := decodeCursor(sinceParam); ok {
			sinceCursor = cur
			haveCursor = true
		}
		// An unparseable cursor falls through to the tailMode bootstrap
		// below rather than failing the request -- a caller with a
		// corrupt/foreign `since` should still get a usable response.
	}

	sharelogBase := filepath.Dir(logPath)
	minDir := ""
	if haveCursor {
		minDir = sinceCursor.dir
	}
	dirs := listHeightDirs(sharelogBase, minDir)
	if len(dirs) == 0 && haveCursor {
		// Nothing at or above the cursor's own height -- an unusual cursor
		// (older than the pool's actual dirs, or naming a height that never
		// existed). Fall back to the full unbounded listing so resolution
		// still matches pre-bounding behaviour exactly; this path is rare,
		// so paying the full-retention cost here is fine.
		dirs = listHeightDirs(sharelogBase, "")
	}
	if len(dirs) == 0 {
		// Pool has not found/started work on a block yet -- not an error.
		responseCache.Set(cacheKey, response)
		json.NewEncoder(w).Encode(response)
		return
	}
	currentDir := dirs[len(dirs)-1]
	if h, err := strconv.ParseInt(currentDir, 16, 64); err == nil {
		height := int(h)
		response.Height = &height
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var startDir, startFile string
	var startOffset int64
	tailMode := true

	if haveCursor {
		startDir, startFile, startOffset = resolveCursorStart(sharelogBase, dirs, sinceCursor)
		tailMode = false
	} else {
		startDir, startFile, startOffset = currentDir, "", 0
	}

	var userFilterSet map[string]struct{}
	if userFilter != "" {
		spellings := cashaddrSpellings(userFilter)
		userFilterSet = make(map[string]struct{}, len(spellings))
		for _, s := range spellings {
			userFilterSet[s] = struct{}{}
		}
	}

	records, cursor := scanShares(ctx, sharelogBase, dirs, startDir, startFile, startOffset, userFilterSet, limit, tailMode)

	response.Shares = records
	response.NextCursor = encodeCursor(cursor)

	responseCache.Set(cacheKey, response)
	json.NewEncoder(w).Encode(response)
}

// handlePing answers a browser's round-trip-time probe with an empty 204. It must
// do ZERO work beyond responding -- no body read, no disk I/O, and no log
// line -- because any of those is added to the latency being measured. The
// request's query string (the consumer appends `?t=<ts>` to defeat caching)
// is never read. Auth and rate limiting happen in pingMiddleware before this
// runs; this function assumes both already passed.
func handlePing(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	logExists := false
	var logSize int64 = 0

	if info, err := os.Stat(logPath); err == nil {
		logExists = true
		logSize = info.Size()
	}

	json.NewEncoder(w).Encode(HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().Unix(),
		LogExists: logExists,
		LogSize:   logSize,
		Uptime:    int64(time.Since(serverStartTime).Seconds()),
		Version:   "3.0.0-go",
	})
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	metricsMutex.RLock()
	requests := requestCounter
	errors := errorCounter
	metricsMutex.RUnlock()

	rateLimiter.mu.RLock()
	rateLimitEntries := len(rateLimiter.clients)
	rateLimiter.mu.RUnlock()

	responseCache.mu.Lock()
	cacheEntries := len(responseCache.cache)
	responseCache.mu.Unlock()

	json.NewEncoder(w).Encode(MetricsResponse{
		Uptime:           int64(time.Since(serverStartTime).Seconds()),
		RequestsTotal:    requests,
		ErrorsTotal:      errors,
		RateLimitEntries: rateLimitEntries,
		CacheEntries:     cacheEntries,
		Timestamp:        time.Now().Unix(),
	})
}

func coinbaseLegacyCacheKey(username string) string {
	return "coinbase:" + username
}

func coinbaseCandidateCacheKey(username string) string {
	// Include the username length so an unvalidated username containing our
	// separators cannot collide with another namespace. There is deliberately
	// only one candidate-scoped proof per username: a newly decoded candidate
	// replaces the old one instead of retaining stale-height history.
	return fmt.Sprintf("coinbase-candidate:%d:%s", len(username), username)
}

func coinbaseFlightRequestKey(candidateHeight *int) string {
	if candidateHeight == nil {
		return "legacy"
	}

	return fmt.Sprintf("height:%d", *candidateHeight)
}

func parseCandidateHeight(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}

	height, err := strconv.ParseUint(raw, 10, 31)
	if err != nil || height == 0 {
		return nil, fmt.Errorf("candidate_height must be a positive decimal block height")
	}

	parsed := int(height)
	return &parsed, nil
}

func cachedCoinbaseProof(username string, candidateHeight *int) (coinbaseFetchResult, bool) {
	if candidateHeight != nil {
		cached, found := responseCache.GetPersistent(coinbaseCandidateCacheKey(username))
		if !found {
			return coinbaseFetchResult{}, false
		}

		response, ok := cached.(CoinbaseResponse)
		if !ok || response.Error != "" || response.BlockHeight == nil || *response.BlockHeight != *candidateHeight {
			return coinbaseFetchResult{}, false
		}

		return coinbaseFetchResult{response: response, status: http.StatusOK}, true
	}

	cached, found := responseCache.Get(coinbaseLegacyCacheKey(username))
	if !found {
		return coinbaseFetchResult{}, false
	}

	response, ok := cached.(CoinbaseResponse)
	if !ok || response.Error != "" || time.Now().Unix()-response.Timestamp >= 10 {
		return coinbaseFetchResult{}, false
	}

	return coinbaseFetchResult{response: response, status: http.StatusOK}, true
}

func cacheCoinbaseProof(result coinbaseFetchResult) {
	if result.status != http.StatusOK || result.response.Error != "" {
		return
	}

	responseCache.Set(coinbaseLegacyCacheKey(result.response.Username), result.response)
	if result.response.BlockHeight != nil {
		responseCache.Set(
			coinbaseCandidateCacheKey(result.response.Username),
			result.response,
		)
	}
}

func getCoinbaseProof(
	ctx context.Context,
	username string,
	candidateHeight *int,
	fetch func(string) coinbaseFetchResult,
) (coinbaseFetchResult, error) {
	return coinbaseFlights.Do(ctx, username, coinbaseFlightRequestKey(candidateHeight), func() coinbaseFetchResult {
		// The cache is checked inside the flight. This closes the race where two
		// requests both observe a miss before either one registers the probe.
		if cached, found := cachedCoinbaseProof(username, candidateHeight); found {
			return cached
		}

		result := fetch(username)
		cacheCoinbaseProof(result)
		return result
	})
}

func handleCoinbase(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("user")
	if username == "" {
		username = "anonymous"
	}

	candidateHeight, err := parseCandidateHeight(r.URL.Query().Get("candidate_height"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(CoinbaseResponse{
			Username:  username,
			Timestamp: time.Now().Unix(),
			Error:     err.Error(),
		})
		return
	}

	// The Stratum wait can legitimately run up to stratumJobDeadline (the
	// pool's update_interval plus slack). Extend only this request's write
	// deadline so a coalesced follower can receive the shared result too.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(stratumJobDeadline + coinbaseWriteDeadlineSlack)); err != nil {
		// Not fatal: an unsupported ResponseWriter (e.g. a test recorder)
		// just falls back to the server-wide WriteTimeout.
		log.Printf("/coinbase: could not extend write deadline: %v", err)
	}

	result, err := getCoinbaseProof(r.Context(), username, candidateHeight, fetchCoinbaseFromStratum)
	if err != nil {
		return
	}
	if result.status != http.StatusOK {
		w.WriteHeader(result.status)
	}
	json.NewEncoder(w).Encode(result.response)
}

func fetchCoinbaseFromStratum(username string) coinbaseFetchResult {
	response := CoinbaseResponse{
		Username:  username,
		Timestamp: time.Now().Unix(),
	}

	// Connect to stratum
	conn, err := net.DialTimeout("tcp", DefaultStratumHost, 5*time.Second)
	if err != nil {
		response.Error = fmt.Sprintf("Failed to connect to pool: %v", err)
		return coinbaseFetchResult{response: response, status: http.StatusOK}
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(stratumJobDeadline))

	// Send mining.subscribe. The parameter is the USERAGENT, not the username --
	// naming it after the caller keeps these probes identifiable in the pool log.
	subscribe := StratumSubscribe{
		ID:     1,
		Method: "mining.subscribe",
		Params: []interface{}{username + "/api"},
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(subscribe); err != nil {
		response.Error = fmt.Sprintf("Failed to send subscribe: %v", err)
		return coinbaseFetchResult{response: response, status: http.StatusOK}
	}

	// Read responses
	scanner := bufio.NewScanner(conn)
	// mining.notify carries a merkle branch plus both coinbase halves -- well
	// under bufio's default 64KB token cap in practice, but an oversized line
	// would end the scan silently and be misreported below as an authorisation
	// failure. Raise the cap so only a genuine transport fault stops the scan.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var coinbasePart1, coinbasePart2, networkBits string
	var extranonce1 string
	var extranonce2Size int
	authorized := false

	// In btcsolo mode (-B) ckpool builds a workbase PER USER, at authorise time
	// (src/stratifier.c, generate_user()). A client that only subscribes is
	// never issued a mining.notify at all -- measured against this pool
	// 2026-08-28: subscribe-only received zero jobs, subscribe+authorise
	// received the caller's own coinbase. So the username has to be authorised,
	// not merely passed as a useragent, or this endpoint returns nothing.
	//
	// Authorise as "<user>."+ProbeWorkerSuffix so the phantom worker this creates is
	// obviously ours in the miner's stats. canonicalAuthUser both selects the
	// payout token the same way ckpool does (splitting on EITHER '.' or '_',
	// src/stratifier.c:5466) and canonicalises a CashAddr spelling -- so this
	// probe never authorises a bare address ckpool already knows in prefixed
	// form and mints a shadow user.
	authUser := canonicalAuthUser(username) + "." + ProbeWorkerSuffix

	for i := 0; i < 24 && scanner.Scan(); i++ {
		line := scanner.Text()

		// The subscribe reply carries extranonce1 and extranonce2_size, both of
		// which are needed to rebuild the coinbase (see below).
		var resp struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal([]byte(line), &resp) == nil && resp.ID == 1 && len(resp.Result) > 0 {
			var parts []interface{}
			if json.Unmarshal(resp.Result, &parts) == nil && len(parts) >= 3 {
				extranonce1, _ = parts[1].(string)
				if f, ok := parts[2].(float64); ok {
					extranonce2Size = int(f)
				}
			}
			if err := encoder.Encode(StratumSubscribe{
				ID:     2,
				Method: "mining.authorize",
				Params: []interface{}{authUser, "x"},
			}); err != nil {
				response.Error = fmt.Sprintf("Failed to send authorize: %v", err)
				return coinbaseFetchResult{response: response, status: http.StatusOK}
			}
			continue
		}
		if json.Unmarshal([]byte(line), &resp) == nil && resp.ID == 2 {
			var ok bool
			if json.Unmarshal(resp.Result, &ok) == nil {
				authorized = ok
			}
			continue
		}

		var notify StratumNotify
		if err := json.Unmarshal([]byte(line), &notify); err == nil {
			if notify.Method == "mining.notify" && len(notify.Params) >= 9 {
				if cb1, ok := notify.Params[2].(string); ok {
					coinbasePart1 = cb1
				}
				if cb2, ok := notify.Params[3].(string); ok {
					coinbasePart2 = cb2
				}
				if bits, ok := notify.Params[6].(string); ok {
					networkBits = bits
				}
				// Only a post-authorise job is this user's job.
				if authorized {
					break
				}
			}
		}
	}

	// Distinguish a transport fault from an authorisation refusal. The
	// stratumJobDeadline set at connect can expire mid-conversation, ending
	// the scan with Scan()==false; without this the handler falls through
	// and blames the username, sending an operator to debug an auth problem
	// that does not exist.
	if err := scanner.Err(); err != nil {
		// The read can die on either side of the authorise reply, and the two
		// are different faults to chase: no auth yet vs. authorised but the
		// post-authorise mining.notify never arrived.
		stage := "before authorisation completed"
		if authorized {
			stage = "after authorisation, while waiting for a job"
		}
		response.Error = fmt.Sprintf("Stratum read failed %s: %v", stage, err)

		// A read deadline expiring with no job is a real, expected outcome on
		// an idle pool (ckpool only broadcasts every update_interval
		// seconds) -- not a hard transport fault. Report it as 504 with a
		// JSON body the caller can act on, rather than letting the
		// connection drop silently the way the old fixed 10s WriteTimeout
		// race used to.
		status := http.StatusBadGateway
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			status = http.StatusGatewayTimeout
		}
		return coinbaseFetchResult{response: response, status: status}
	}

	if !authorized {
		response.Error = fmt.Sprintf("Pool did not authorise %s -- a username that is neither a valid BCH address nor a plain name is rejected outright", authUser)
		return coinbaseFetchResult{response: response, status: http.StatusOK}
	}

	if coinbasePart1 == "" || coinbasePart2 == "" {
		response.Error = "Failed to get coinbase from pool"
		return coinbaseFetchResult{response: response, status: http.StatusOK}
	}

	// Rebuild the coinbase exactly as a miner does:
	//     coinb1 + extranonce1 + extranonce2 + coinb2
	//
	// coinb1 already declares the FULL scriptSig length, extranonce included, so
	// concatenating coinb1+coinb2 alone leaves the parser 12 bytes short (4 for
	// extranonce1, 8 for extranonce2) and every output offset after it is wrong.
	// Measured against this pool 2026-08-28, the truncated form parsed as ONE
	// output of -5970863856677875828 sats (-59,708,638,566 BCH) instead of the
	// real two-output 98/2 split. extranonce2 is zero-filled: its value does not
	// affect the outputs, only its length matters for the offsets.
	response.CoinbaseHex = coinbasePart1 + extranonce1 +
		strings.Repeat("00", extranonce2Size) + coinbasePart2
	response.NetworkBits = networkBits

	// Extract coinbase message from part1
	if msg := extractCoinbaseMessage(coinbasePart1); msg != "" {
		response.CoinbaseMessage = msg
	}

	// Extract block height from part1
	if height := extractBlockHeight(coinbasePart1); height != nil {
		response.BlockHeight = height
	}

	// Parse outputs from the FULL coinbase transaction (coinb1+coinb2).
	// coinb2 alone is ~12 bytes and cannot contain the outputs — parsing it
	// was what made /coinbase report a single bogus multi-million-BCH output.
	outputs, totalValue := parseCoinbaseOutputs(response.CoinbaseHex)
	response.Outputs = outputs
	response.TotalValue = totalValue
	response.TotalValueBCH = fmt.Sprintf("%.8f", float64(totalValue)/100000000.0)

	return coinbaseFetchResult{response: response, status: http.StatusOK}
}

// readVarint reads a Bitcoin compact-size integer at data[offset].
//
// Encoding: a first byte < 0xfd is the value itself; 0xfd means a 2-byte
// little-endian value follows; 0xfe a 4-byte one; 0xff an 8-byte one.
// Returns the value, the offset just past it, and false when the buffer is
// too short (callers must abort — this parses network-supplied data).
func readVarint(data []byte, offset int) (uint64, int, bool) {
	if offset < 0 || offset >= len(data) {
		return 0, offset, false
	}

	prefix := data[offset]
	offset++

	switch {
	case prefix < 0xfd:
		return uint64(prefix), offset, true
	case prefix == 0xfd:
		if offset+2 > len(data) {
			return 0, offset, false
		}
		v := uint64(data[offset]) | uint64(data[offset+1])<<8
		return v, offset + 2, true
	case prefix == 0xfe:
		if offset+4 > len(data) {
			return 0, offset, false
		}
		v := uint64(data[offset]) | uint64(data[offset+1])<<8 |
			uint64(data[offset+2])<<16 | uint64(data[offset+3])<<24
		return v, offset + 4, true
	default: // 0xff
		if offset+8 > len(data) {
			return 0, offset, false
		}
		v := uint64(data[offset]) | uint64(data[offset+1])<<8 |
			uint64(data[offset+2])<<16 | uint64(data[offset+3])<<24 |
			uint64(data[offset+4])<<32 | uint64(data[offset+5])<<40 |
			uint64(data[offset+6])<<48 | uint64(data[offset+7])<<56
		return v, offset + 8, true
	}
}

// coinbaseScriptSig returns the scriptSig of the coinbase transaction's single
// input, plus the offset in data just past that input (i.e. where the output
// count varint starts).
//
// A raw transaction is laid out as:
//
//	version(4) | varint input_count | inputs | varint output_count | outputs | locktime(4)
//
// and each input as:
//
//	prevout_hash(32) | prevout_index(4) | varint script_len | script | sequence(4)
//
// This tolerates a TRUNCATED buffer on purpose: stratum's coinb1 is only the
// prefix of the transaction, cut in the middle of the scriptSig, so the
// declared script length routinely exceeds what we hold. In that case the
// available tail is returned and ok is false for the trailing offset.
func coinbaseScriptSig(data []byte) (script []byte, next int, ok bool) {
	offset := 4 // version
	if len(data) < offset {
		return nil, 0, false
	}

	inputCount, offset, ok := readVarint(data, offset)
	if !ok || inputCount == 0 {
		return nil, 0, false
	}

	// prevout hash + index
	if offset+36 > len(data) {
		return nil, 0, false
	}
	offset += 36

	scriptLen, offset, ok := readVarint(data, offset)
	if !ok {
		return nil, 0, false
	}

	end := offset + int(scriptLen)
	if scriptLen > uint64(len(data)) || end < offset || end > len(data) {
		// Truncated (normal for coinb1) — hand back what we actually have.
		return data[offset:], 0, false
	}

	// Skip the 4-byte sequence to land on the output count.
	next = end + 4
	if next > len(data) {
		return data[offset:end], 0, false
	}
	return data[offset:end], next, true
}

// skipCoinbaseHeightPush drops the BIP34 block-height push (a 0x01–0x04 push
// opcode followed by that many little-endian bytes) from the front of a
// coinbase scriptSig, returning the remaining signature bytes.
func skipCoinbaseHeightPush(script []byte) []byte {
	if len(script) == 0 {
		return script
	}
	n := int(script[0])
	if n >= 1 && n <= 4 && len(script) >= 1+n {
		return script[1+n:]
	}
	return script
}

// longestPrintableRun returns the longest maximal run of printable ASCII in b.
// Coinbase scriptSigs interleave the pool signature with binary extranonce
// bytes, so the signature is recovered as the longest readable run rather than
// by stopping at the first delimiter — the previous implementation truncated
// the tag at the first "/", returning "BlockSniper.ai/" rather than the full
// "BlockSniper.ai/[Solo]".
func longestPrintableRun(b []byte) string {
	best, start := "", -1
	for i := 0; i <= len(b); i++ {
		printable := i < len(b) && b[i] >= 0x20 && b[i] <= 0x7e
		if printable {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if run := string(b[start:i]); len(run) > len(best) {
				best = run
			}
			start = -1
		}
	}
	return best
}

// extractCoinbaseMessage recovers the pool signature from the coinbase input
// script. Accepts either the full coinbase transaction or stratum's coinb1.
func extractCoinbaseMessage(hexStr string) string {
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return ""
	}

	script, _, _ := coinbaseScriptSig(data)
	if len(script) == 0 {
		return ""
	}

	msg := longestPrintableRun(skipCoinbaseHeightPush(script))
	if len(msg) < 3 {
		return ""
	}
	return msg
}

// extractBlockHeight reads the BIP34 height push at the head of the coinbase
// scriptSig. Accepts either the full coinbase transaction or coinb1.
func extractBlockHeight(hexStr string) *int {
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil
	}

	script, _, _ := coinbaseScriptSig(data)
	if len(script) == 0 {
		return nil
	}

	n := int(script[0])
	if n < 1 || n > 4 || len(script) < 1+n {
		return nil
	}

	height := 0
	for i := 0; i < n; i++ {
		height |= int(script[1+i]) << (8 * i)
	}
	if height <= 0 {
		return nil
	}
	return &height
}

// parseCoinbaseOutputs walks a FULL raw coinbase transaction (coinb1+coinb2)
// and returns its outputs plus their summed value.
//
// It previously received only coinb2 — roughly 12 bytes, which cannot contain
// any output — and treated data[0] (the first byte of the version field) as an
// output count, so /coinbase reported one bogus multi-million-BCH output with
// an empty address. Every length here is a varint and every read is
// bounds-checked; malformed or truncated input yields no outputs, never a panic.
func parseCoinbaseOutputs(hexStr string) ([]CoinbaseOutput, int64) {
	outputs := []CoinbaseOutput{}

	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return outputs, 0
	}

	_, offset, ok := coinbaseScriptSig(data)
	if !ok {
		return outputs, 0
	}

	outputCount, offset, ok := readVarint(data, offset)
	if !ok || outputCount == 0 || outputCount > uint64(len(data)) {
		return outputs, 0
	}

	// A short buffer degrades gracefully: whatever outputs were fully read are
	// kept and the walk stops. Captured coinbases are routinely clipped a few
	// bytes into the final OP_RETURN commitment, and dropping the two real
	// payouts because of that would be worse than reporting them.
	var totalValue int64
	for i := uint64(0); i < outputCount; i++ {
		if offset+8 > len(data) {
			break
		}
		value := int64(binary.LittleEndian.Uint64(data[offset:]))
		offset += 8

		scriptLen, next, ok := readVarint(data, offset)
		if !ok {
			break
		}
		offset = next

		end := offset + int(scriptLen)
		if end < offset || end > len(data) {
			// Truncated scriptPubKey — take the bytes we hold, then stop.
			end = len(data)
		}
		script := data[offset:end]
		offset = end

		out := CoinbaseOutput{
			Value:    value,
			ValueBCH: fmt.Sprintf("%.8f", float64(value)/100000000.0),
		}

		switch {
		case len(script) > 0 && script[0] == 0x6a:
			// OP_RETURN — an unspendable commitment, not a payout.
			out.Type = "op_return"
		case len(script) == 25 && script[0] == 0x76 && script[1] == 0xa9 &&
			script[2] == 0x14 && script[23] == 0x88 && script[24] == 0xac:
			// P2PKH: OP_DUP OP_HASH160 <20> OP_EQUALVERIFY OP_CHECKSIG
			out.Address = encodeLegacyAddress(script[3:23])
			out.Type = "pool_fee"
		default:
			out.Type = "pool_fee"
		}

		outputs = append(outputs, out)
		totalValue += value
	}

	// The payout order is not fixed — the fee output can come first — so the
	// miner output is identified by value, not by index: the largest
	// non-OP_RETURN output is the block reward.
	best := -1
	for i := range outputs {
		if outputs[i].Type == "op_return" {
			continue
		}
		if best < 0 || outputs[i].Value > outputs[best].Value {
			best = i
		}
	}
	if best >= 0 {
		outputs[best].Type = "miner"
	}

	return outputs, totalValue
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// encodeLegacyAddress renders a 20-byte HASH160 as a mainnet P2PKH
// Base58Check address (version byte 0x00).
//
// This previously returned fmt.Sprintf("1%x...", pubkeyHash[:8]) — a truncated
// hex placeholder that merely LOOKED like an address. Anything consuming
// /coinbase got a value that could never be paid to or reconciled on-chain.
func encodeLegacyAddress(pubkeyHash []byte) string {
	if len(pubkeyHash) != 20 {
		return ""
	}

	payload := append([]byte{0x00}, pubkeyHash...)
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	full := append(payload, second[:4]...)

	// base58 encode
	num := new(big.Int).SetBytes(full)
	radix := big.NewInt(58)
	zero := big.NewInt(0)
	mod := new(big.Int)
	var out []byte
	for num.Cmp(zero) > 0 {
		num.DivMod(num, radix, mod)
		out = append([]byte{base58Alphabet[mod.Int64()]}, out...)
	}
	// leading zero bytes become '1'
	for _, b := range full {
		if b != 0x00 {
			break
		}
		out = append([]byte{'1'}, out...)
	}
	return string(out)
}

func isASCII(s string) bool {
	for _, c := range s {
		if c > 127 || (c < 32 && c != '\n' && c != '\t') {
			return false
		}
	}
	return len(s) > 0
}

func incrementRequestCounter() {
	metricsMutex.Lock()
	requestCounter++
	metricsMutex.Unlock()
}

func incrementErrorCounter() {
	metricsMutex.Lock()
	errorCounter++
	metricsMutex.Unlock()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	// Load configuration from environment
	// Fail closed. Falling back to DefaultAPIKey would start the server on a
	// key published in this repository, while the startup banner below still
	// reported "[SET]" -- an unauthenticated API that looks authenticated.
	// This endpoint exposes the pool's whole log tree, so refuse instead.
	apiKey = getEnvWithFallback("CASHSTRATUM_API_KEY", "CKPOOL_API_KEY")
	if apiKey == "" || apiKey == DefaultAPIKey {
		log.Fatal("CASHSTRATUM_API_KEY (or CKPOOL_API_KEY) is unset or still the placeholder value. " +
			"Generate one with: openssl rand -hex 32")
	}

	logPath = getEnvWithFallback("CASHSTRATUM_LOG_PATH", "CKPOOL_LOG_PATH")
	if logPath == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			csPath := filepath.Join(homeDir, "cashstratum", "logs", "cashstratum.log")
			ckPath := filepath.Join(homeDir, "ckpool", "logs", "ckpool.log")
			if _, err := os.Stat(csPath); err == nil {
				logPath = csPath
			} else if _, err := os.Stat(ckPath); err == nil {
				logPath = ckPath
			} else {
				logPath = csPath
			}
		} else {
			logPath = "/var/log/cashstratum/cashstratum.log" // Fallback
		}
	}

	userLogPath = getEnvWithFallback("CASHSTRATUM_USER_LOGS_PATH", "CKPOOL_USER_LOGS_PATH")
	if userLogPath == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			csUsers := filepath.Join(homeDir, "cashstratum", "logs", "users")
			ckUsers := filepath.Join(homeDir, "ckpool", "logs", "users")
			if _, err := os.Stat(csUsers); err == nil {
				userLogPath = csUsers
			} else if _, err := os.Stat(ckUsers); err == nil {
				userLogPath = ckUsers
			} else {
				userLogPath = csUsers
			}
		} else {
			userLogPath = "/var/log/cashstratum/users" // Fallback
		}
	}

	// The prefix canonicalAuthUser prepends to a bare CashAddr username.
	// Production is mainnet and needs nothing; a regtest/testnet4 rig
	// sets this to "bchreg"/"bchtest".
	if v := getEnvWithFallback("CASHSTRATUM_CASHADDR_PREFIX", "CKPOOL_CASHADDR_PREFIX"); v != "" {
		cashaddrPrefix = v
	}

	port = getEnvWithFallback("CASHSTRATUM_API_PORT", "CKPOOL_API_PORT")
	if port == "" {
		port = DefaultPort
	}

	// Parse rate limit budgets from environment
	rateLimitRequests = parseRateLimitEnvWithFallback("CASHSTRATUM_RATE_LIMIT", "CKPOOL_RATE_LIMIT", DefaultRateLimitRequests)
	sharesRateLimitRequests = parseRateLimitEnvWithFallback("CASHSTRATUM_SHARES_RATE_LIMIT", "CKPOOL_SHARES_RATE_LIMIT", DefaultSharesRateLimitRequests)
	pingRateLimitRequests = parseRateLimitEnvWithFallback("CASHSTRATUM_PING_RATE_LIMIT", "CKPOOL_PING_RATE_LIMIT", DefaultPingRateLimitRequests)

	// Initialize rate limiters with the parsed budgets
	rateLimiter = NewRateLimiter(rateLimitRequests)
	sharesRateLimiter = NewRateLimiter(sharesRateLimitRequests)
	pingRateLimiter = NewRateLimiter(pingRateLimitRequests)

	// Node RPC credentials come from cashstratum.conf/ckpool.conf rather than this service's
	// own environment: the pool cannot run without them, so they are already
	// on the host, already correct, and rotate with the node instead of
	// drifting out of a second copy.
	confPath := getEnvWithFallback("CASHSTRATUM_CONF_PATH", "CKPOOL_CONF_PATH")
	if confPath == "" {
		for _, candidate := range ckconf.Candidates(logPath) {
			if _, err := os.Stat(candidate); err == nil {
				confPath = candidate
				break
			}
		}
	}
	if confPath == "" {
		log.Printf("Node RPC: no conf found near %s and CASHSTRATUM_CONF_PATH/CKPOOL_CONF_PATH unset -- /stats will fall back to log-scraped height", logPath)
		log.Printf("Stratum job deadline: no conf -- using default update_interval (%s) + slack = %s", DefaultUpdateInterval, stratumJobDeadline)
	} else {
		btcdRPC = ckconf.LoadBtcdRPC(confPath)
		stratumJobDeadline = ckconf.LoadUpdateInterval(confPath) + stratumDeadlineSlack
		log.Printf("Stratum job deadline: %s (update_interval from %s + %s slack)", stratumJobDeadline, confPath, stratumDeadlineSlack)
	}

	// Log startup information
	log.Printf("Starting CashStratum Log API (Go Version) on port %s", port)
	log.Printf("Log path: %s", logPath)
	log.Printf("User logs path: %s", userLogPath)
	// Unconditionally reachable only because startup now aborts on a missing
	// or placeholder key, so this is a fact rather than an assumption.
	log.Printf("API Key: [SET]")
	log.Printf("Rate limit: %d req/min (general), %d req/min (/shares), %d req/min (/ping)", rateLimitRequests, sharesRateLimitRequests, pingRateLimitRequests)
	log.Printf("Version: 3.0.0-go")
	log.Printf("Features: High performance, low memory, concurrent requests")

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/tail", authMiddleware(handleTail))
	mux.HandleFunc("/grep", authMiddleware(handleGrep))
	mux.HandleFunc("/find-block", authMiddleware(handleFindBlock))
	mux.HandleFunc("/user-file", authMiddleware(handleUserFile))
	mux.HandleFunc("/stats", authMiddleware(handleStats))
	mux.HandleFunc("/user-log", authMiddleware(handleUserLog))
	mux.HandleFunc("/coinbase", authMiddleware(handleCoinbase))
	mux.HandleFunc("/shares", authMiddlewareWithLimiter(sharesRateLimiter, handleShares))
	mux.HandleFunc("/health", authMiddleware(handleHealth))
	mux.HandleFunc("/metrics", authMiddleware(handleMetrics))
	// Deliberately unauthenticated -- see pingMiddleware for why /health cannot
	// be reused for this instead.
	mux.HandleFunc("/ping", pingMiddleware(handlePing))

	// Create server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	// Start server
	log.Printf("Server listening on 0.0.0.0:%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}

	log.Println("Server stopped")
}
