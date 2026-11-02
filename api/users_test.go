package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const usersPoolStatus = `{"runtime": 100, "lastupdate": 1793000000, "Users": 3, "Workers": 4, "Idle": 1, "Disconnected": 0}
{"hashrate1m": "108P", "hashrate5m": "107P", "hashrate15m": "106P", "hashrate1hr": "105P", "hashrate6hr": "1P", "hashrate1d": "1P", "hashrate7d": "1P"}
{"diff": 1.5, "luck": 83.04, "accepted": 123456789012, "rejected": 5, "bestshare": 9.5, "SPS1m": 1.1, "SPS5m": 1.2, "SPS15m": 1.3, "SPS1h": 1.4}
`

const usersAddrA = "bitcoincash:qqfvksaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const usersAddrB = "qqfvksbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func usersFile(lastshare int64, workers int, probe bool) string {
	probeEntry := ""
	if probe {
		probeEntry = `, {"workername": "` + usersAddrA + `.` + ProbeWorkerSuffix + `", "shares": 1}`
	}
	return `{"hashrate1m": "1.5T", "hashrate5m": "1.4T", "hashrate1hr": "1.3T", "workers": ` +
		itoa(workers) + `, "lastshare": ` + itoa(int(lastshare)) + `, "luck": 0.0000147123456, "shares": 123456789012,
	"worker": [{"workername": "` + usersAddrA + `.rig1", "shares": 5}` + probeEntry + `]}`
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// usersEnv lays out <tmp>/users and <tmp>/pool/pool.status and points the
// package globals and a fresh cache (with a fake clock) at it.
type usersEnv struct {
	t     *testing.T
	root  string
	users string
	clock time.Time
	cache *usersSnapshotCache
}

func newUsersEnv(t *testing.T) *usersEnv {
	t.Helper()
	root := t.TempDir()
	env := &usersEnv{t: t, root: root, users: filepath.Join(root, "users"), clock: time.Unix(1793000100, 0)}
	for _, d := range []string{env.users, filepath.Join(root, "pool")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env.writePool(usersPoolStatus)

	origPath, origCache := userLogPath, usersCache
	userLogPath = env.users
	env.cache = newUsersSnapshotCache()
	env.cache.now = func() time.Time { return env.clock }
	env.cache.retryDelay = time.Millisecond
	usersCache = env.cache
	t.Cleanup(func() { userLogPath, usersCache = origPath, origCache })
	return env
}

func (e *usersEnv) writePool(body string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, "pool", "pool.status"), []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *usersEnv) writeUser(name, body string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.users, name), []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *usersEnv) get(target string) (*httptest.ResponseRecorder, map[string]any) {
	e.t.Helper()
	rec := httptest.NewRecorder()
	handleUsers(rec, httptest.NewRequest(http.MethodGet, target, nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		e.t.Fatalf("body not JSON: %v: %s", err, rec.Body.String())
	}
	return rec, body
}

func userNames(body map[string]any) []string {
	var out []string
	for _, u := range body["users"].([]any) {
		out = append(out, u.(map[string]any)["user"].(string))
	}
	return out
}

// Item 1: shape, raw pool keys/values, user = file name verbatim.
func TestUsersReturnsPoolAndUsersRaw(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser(usersAddrA, usersFile(1793000090, 2, false))
	env.writeUser(usersAddrB, usersFile(1793000000, 1, false))

	rec, body := env.get("/users")
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	pool := body["pool"].(map[string]any)
	if pool["hashrate5m"] != "107P" || pool["luck"] != 83.04 || pool["Users"] != float64(3) || pool["SPS1m"] != 1.1 {
		t.Errorf("pool not passed through raw: %v", pool)
	}
	if body["timestamp"] != float64(env.clock.Unix()) {
		t.Errorf("timestamp = %v", body["timestamp"])
	}
	if body["skipped"] != float64(0) {
		t.Errorf("skipped = %v", body["skipped"])
	}
	if got := userNames(body); !reflect.DeepEqual(got, []string{usersAddrA, usersAddrB}) {
		t.Errorf("users = %v (want sorted, prefix verbatim)", got)
	}
	u := body["users"].([]any)[0].(map[string]any)
	if u["hashrate5m"] != "1.4T" || u["luck"] != 0.0000147123456 || u["workers"] != float64(2) || u["lastshare"] != float64(1793000090) {
		t.Errorf("user keys not raw: %v", u)
	}
}

// Large integers must keep their exact digits in the pool object.
func TestUsersPoolKeepsLargeIntegers(t *testing.T) {
	env := newUsersEnv(t)
	rec, _ := env.get("/users")
	if !contains(rec.Body.String(), `"accepted":123456789012`) {
		t.Errorf("accepted mangled: %s", rec.Body.String())
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && indexOf(s, sub) >= 0 }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Item 2 + 11: worker only with workers=1|true, probe filtered, count untouched
// by the filter semantics of /user-file (count is file's minus dropped probe).
func TestUsersWorkersOptIn(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser(usersAddrA, usersFile(1793000090, 2, true))

	_, body := env.get("/users")
	if _, ok := body["users"].([]any)[0].(map[string]any)["worker"]; ok {
		t.Error("worker present without workers=1")
	}

	for _, q := range []string{"/users?workers=1", "/users?workers=true"} {
		_, body = env.get(q)
		u := body["users"].([]any)[0].(map[string]any)
		ws, ok := u["worker"].([]any)
		if !ok || len(ws) != 1 {
			t.Fatalf("%s: worker = %v, want probe filtered", q, u["worker"])
		}
		if ws[0].(map[string]any)["worker"] != "rig1" {
			t.Errorf("%s: short name missing: %v", q, ws[0])
		}
		// D9: /users keeps the file's own count even with the probe filtered.
		if u["workers"] != float64(2) {
			t.Errorf("%s: workers = %v, want file value 2", q, u["workers"])
		}
	}

	// Without a probe worker the count is exactly the file's own value (D9).
	env.writeUser(usersAddrA, usersFile(1793000090, 0, false))
	env.clock = env.clock.Add(time.Minute)
	_, body = env.get("/users?workers=1")
	if got := body["users"].([]any)[0].(map[string]any)["workers"]; got != float64(0) {
		t.Errorf("workers = %v, want file value 0", got)
	}
}

// D9 regression: with workers=1 the probe is dropped from worker[] but the
// top-level "workers" keeps the file's own value, with or without the param.
func TestUsersWorkersCountStaysFileValueWhenProbeFiltered(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser(usersAddrA, usersFile(1793000090, 2, true))

	_, body := env.get("/users?workers=1")
	u := body["users"].([]any)[0].(map[string]any)
	ws, ok := u["worker"].([]any)
	if !ok || len(ws) != 1 {
		t.Fatalf("worker = %v, want probe removed leaving 1 entry", u["worker"])
	}
	if name := ws[0].(map[string]any)["workername"]; name != usersAddrA+".rig1" {
		t.Errorf("remaining worker = %v, want rig1", name)
	}
	if u["workers"] != float64(2) {
		t.Errorf("workers with workers=1 = %v, want file value 2", u["workers"])
	}

	_, body = env.get("/users")
	u = body["users"].([]any)[0].(map[string]any)
	if u["workers"] != float64(2) {
		t.Errorf("workers without param = %v, want file value 2", u["workers"])
	}
}

// Item 3: lastshare >= active_since; missing lastshare excluded; bad values 400.
func TestUsersActiveSince(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser("a", usersFile(1000, 1, false))
	env.writeUser("b", usersFile(2000, 1, false))
	env.writeUser("c", `{"workers": 1, "hashrate1m": "1T"}`) // never shared

	cases := map[string][]string{
		"/users":                   {"a", "b", "c"},
		"/users?active_since=1000": {"a", "b"},
		"/users?active_since=1001": {"b"},
		"/users?active_since=2001": nil,
		"/users?active_since=0":    {"a", "b"},
	}
	for target, want := range cases {
		_, body := env.get(target)
		if got := userNames(body); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", target, got, want)
		}
	}
	for _, bad := range []string{"abc", "-5", "1.5"} {
		rec, body := env.get("/users?active_since=" + bad)
		if rec.Code != 400 || body["error"] == nil {
			t.Errorf("active_since=%s: code %d body %v", bad, rec.Code, body)
		}
	}
}

// Item 4: Bearer required; rate-limit headers; one request = one bucket slot.
func TestUsersAuthAndRateLimitHeaders(t *testing.T) {
	key := useTestAPIKey(t)
	useFreshRateLimiter(t)
	newUsersEnv(t)
	mux := newMux()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/users", "wrong", "198.51.100.77:1"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad key = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users", nil))
	if rec.Code != http.StatusUnauthorized || contains(rec.Body.String(), "pool") {
		t.Errorf("no key = %d %q, want 401 and no data", rec.Code, rec.Body.String())
	}

	var prev int
	for i := 0; i < 2; i++ {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/users", key, "198.51.100.78:1"))
		if rec.Code != 200 {
			t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Error("X-RateLimit-Limit missing")
		}
		rem := atoiT(t, rec.Header().Get("X-RateLimit-Remaining"))
		if i == 1 && prev-rem != 1 {
			t.Errorf("remaining %d -> %d, want a drop of exactly 1", prev, rem)
		}
		prev = rem
	}
}

func atoiT(t *testing.T, s string) int {
	t.Helper()
	var n int
	if err := json.Unmarshal([]byte(s), &n); err != nil {
		t.Fatalf("not an int: %q", s)
	}
	return n
}

// Item 6: filters share one directory read; a changed pool.status is picked up.
func TestUsersCacheSharesReadAndRefreshesOnPoolChange(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser("a", usersFile(1000, 1, false))
	env.writeUser("b", usersFile(2000, 1, false))

	env.get("/users")
	env.get("/users?active_since=1500")
	env.get("/users?active_since=500&workers=1")
	if env.cache.dirReads != 1 {
		t.Errorf("dirReads = %d, want 1 inside the TTL", env.cache.dirReads)
	}
	// Query-specific results must not leak between callers.
	if _, body := env.get("/users?active_since=1500"); !reflect.DeepEqual(userNames(body), []string{"b"}) {
		t.Errorf("filtered = %v", userNames(body))
	}
	if _, body := env.get("/users"); len(userNames(body)) != 2 {
		t.Errorf("unfiltered = %v", userNames(body))
	}

	// pool.status changes (different size) inside the TTL: next call reflects it.
	env.writePool(usersPoolStatus + "\n")
	env.writePool(`{"runtime": 160, "lastupdate": 1793000060, "Users": 9, "Workers": 4, "Idle": 1, "Disconnected": 0}
{"hashrate1m": "9P", "hashrate5m": "9P", "hashrate15m": "9P", "hashrate1hr": "9P", "hashrate6hr": "1P", "hashrate1d": "1P", "hashrate7d": "1P"}
{"diff": 1.5, "luck": 90.1, "accepted": 1, "rejected": 5, "bestshare": 9.5, "SPS1m": 1.1, "SPS5m": 1.2, "SPS15m": 1.3, "SPS1h": 1.4}
`)
	_, body := env.get("/users")
	if body["pool"].(map[string]any)["lastupdate"] != float64(1793000060) {
		t.Errorf("pool change not reflected: %v", body["pool"])
	}
	if env.cache.dirReads != 2 {
		t.Errorf("dirReads = %d, want 2 after pool change", env.cache.dirReads)
	}

	// TTL expiry re-reads even with identical pool.status.
	env.clock = env.clock.Add(31 * time.Second)
	env.get("/users")
	if env.cache.dirReads != 3 {
		t.Errorf("dirReads = %d, want 3 after TTL", env.cache.dirReads)
	}
}

// Item 7: torn user file -> 200, skipped:1 with no good copy, last good copy otherwise.
func TestUsersTornUserFile(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser("good", usersFile(1000, 1, false))
	env.writeUser("torn", `{"hashrate1m": "1T", "worker": [{"workern`)
	env.writeUser("empty", ``)

	rec, body := env.get("/users")
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	if body["skipped"] != float64(2) {
		t.Errorf("skipped = %v, want 2", body["skipped"])
	}
	if got := userNames(body); !reflect.DeepEqual(got, []string{"good"}) {
		t.Errorf("users = %v", got)
	}

	// A user with a last good copy survives a later torn write.
	env.writeUser("torn", usersFile(1500, 1, false))
	env.clock = env.clock.Add(time.Minute)
	if _, body = env.get("/users"); len(userNames(body)) != 2 {
		t.Fatalf("users = %v", userNames(body))
	}
	env.writeUser("torn", ``)
	env.clock = env.clock.Add(time.Minute)
	_, body = env.get("/users")
	if got := userNames(body); !reflect.DeepEqual(got, []string{"good", "torn"}) || body["skipped"] != float64(1) {
		// "empty" never had a good copy, so it stays skipped.
		t.Errorf("users = %v skipped = %v", got, body["skipped"])
	}
}

// Dot-files, directories and oversized files are not users.
func TestUsersSkipsDotfilesDirsAndHugeFiles(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser("a", usersFile(1000, 1, false))
	env.writeUser(".hidden", usersFile(1000, 1, false))
	if err := os.Mkdir(filepath.Join(env.users, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	huge := make([]byte, usersMaxFileSize+1)
	for i := range huge {
		huge[i] = ' '
	}
	env.writeUser("huge", string(huge))

	_, body := env.get("/users")
	if got := userNames(body); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("users = %v", got)
	}
	if body["skipped"] != float64(1) {
		t.Errorf("skipped = %v (oversized file counts)", body["skipped"])
	}
}

// Item 9: outages are 503 with headers, never 200 with empty users.
func TestUsersOutageIs503(t *testing.T) {
	key := useTestAPIKey(t)
	useFreshRateLimiter(t)
	env := newUsersEnv(t)
	env.writeUser("a", usersFile(1000, 1, false))
	mux := newMux()
	call := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/users", key, "198.51.100.90:1"))
		return rec
	}
	check := func(label, wantMsg string) {
		t.Helper()
		rec := call()
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != 503 || body["error"] != wantMsg || body["users"] != nil {
			t.Errorf("%s: code %d body %s", label, rec.Code, rec.Body.String())
		}
		// Generic message only: no filesystem paths or OS error text leak out.
		if contains(rec.Body.String(), "/") || contains(rec.Body.String(), "open ") {
			t.Errorf("%s: 503 body leaks detail: %s", label, rec.Body.String())
		}
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Errorf("%s: rate-limit headers missing on 503", label)
		}
	}

	poolPath := filepath.Join(env.root, "pool", "pool.status")
	os.Remove(poolPath)
	check("missing pool.status", "pool status unavailable")

	env.writePool(`{"runtime": 1, "lastup`)
	check("torn pool.status", "pool status unavailable")

	env.writePool(``)
	check("empty pool.status", "pool status unavailable")

	env.writePool(usersPoolStatus)
	os.RemoveAll(env.users)
	check("users dir unreadable", "users directory unavailable")
}

// A pool.status that later goes torn falls back to the last good copy, and a
// frozen daemon keeps answering 200 with the old lastupdate (D8).
func TestUsersFrozenDaemonStaysOK(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser("a", usersFile(1000, 1, false))
	env.get("/users")

	env.writePool(`{"runtime": 1, "lastup`)
	env.clock = env.clock.Add(10 * time.Minute)
	rec, body := env.get("/users")
	if rec.Code != 200 || body["pool"].(map[string]any)["lastupdate"] != float64(1793000000) {
		t.Errorf("code %d pool %v", rec.Code, body["pool"])
	}

	// Untouched files, long after: still 200 with the same lastupdate.
	env.writePool(usersPoolStatus)
	env.clock = env.clock.Add(10 * time.Minute)
	rec, body = env.get("/users")
	if rec.Code != 200 || body["pool"].(map[string]any)["lastupdate"] != float64(1793000000) {
		t.Errorf("code %d pool %v", rec.Code, body["pool"])
	}
}

// Item 10: the served snapshot is never older than the TTL while files change.
func TestUsersSnapshotNeverOlderThanTTL(t *testing.T) {
	env := newUsersEnv(t)
	env.writeUser("a", usersFile(1000, 1, false))
	env.get("/users")
	env.writeUser("a", usersFile(1060, 1, false)) // same size: only the TTL can notice

	env.clock = env.clock.Add(usersCacheTTL - time.Second)
	_, body := env.get("/users")
	if body["users"].([]any)[0].(map[string]any)["lastshare"] != float64(1000) {
		t.Error("expected cached snapshot inside the TTL")
	}
	env.clock = env.clock.Add(2 * time.Second)
	_, body = env.get("/users")
	if body["users"].([]any)[0].(map[string]any)["lastshare"] != float64(1060) {
		t.Error("snapshot older than the TTL was served")
	}
	if usersCacheTTL > 90*time.Second {
		t.Errorf("TTL %v exceeds the 90 s freshness bound", usersCacheTTL)
	}
}

func TestPoolStatusPathDerivedFromUsersDir(t *testing.T) {
	orig := userLogPath
	defer func() { userLogPath = orig }()
	userLogPath = "/srv/pool/logs/users"
	if got := poolStatusPath(); got != "/srv/pool/logs/pool/pool.status" {
		t.Errorf("poolStatusPath = %q", got)
	}
}

func useFreshRateLimiter(t *testing.T) {
	t.Helper()
	orig := rateLimiter
	rateLimiter = NewRateLimiter(DefaultRateLimitRequests)
	t.Cleanup(func() { rateLimiter = orig })
}
