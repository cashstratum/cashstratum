package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"cashstratumapi/internal/poolfmt"
)

const (
	// usersCacheTTL bounds how long a snapshot is reused. The daemon rewrites
	// the files every 60 s, so a 30 s TTL keeps responses well inside the
	// ~90 s freshness bound. A changed pool.status is also noticed on every
	// request (see usersSnapshotCache.get), so the TTL is only the ceiling.
	usersCacheTTL = 30 * time.Second
	// usersMaxFileSize matches /user-file's cap.
	usersMaxFileSize = 1024 * 1024
	// usersRetryDelay is the pause before the single retry of a torn read.
	usersRetryDelay = 20 * time.Millisecond
)

// usersEntry is one user file in a snapshot. The raw bytes are kept (not the
// decoded map) because the per-request worker filters mutate the decoded
// form; each request decodes its own copy.
type usersEntry struct {
	name      string
	raw       []byte
	lastshare float64
	hasLast   bool
}

type usersSnapshot struct {
	at      time.Time
	statMod time.Time // pool.status mtime when read
	statLen int64
	pool    map[string]interface{}
	users   []usersEntry
	skipped int
}

// usersSnapshotCache holds the last snapshot plus the last good copy of every
// file, so a torn read (the daemon truncates then rewrites in place) falls
// back to the previous content instead of dropping the user.
type usersSnapshotCache struct {
	mu         sync.Mutex
	now        func() time.Time
	ttl        time.Duration
	retryDelay time.Duration

	snap     *usersSnapshot
	goodPool map[string]interface{}
	goodUser map[string]usersEntry
	dirReads int // number of users-dir scans, observable by tests
}

func newUsersSnapshotCache() *usersSnapshotCache {
	return &usersSnapshotCache{
		now:        time.Now,
		ttl:        usersCacheTTL,
		retryDelay: usersRetryDelay,
		goodUser:   map[string]usersEntry{},
	}
}

var usersCache = newUsersSnapshotCache()

// poolStatusPath is the daemon's pool.status. The daemon writes users/ and
// pool/ side by side under one log dir, so the users dir's parent is the log
// dir. This is preferred over filepath.Dir(logPath): the log file can be
// pointed elsewhere independently, while users/ is always <logdir>/users.
func poolStatusPath() string {
	return filepath.Join(filepath.Dir(filepath.Clean(userLogPath)), "pool", "pool.status")
}

// readValidated reads path and retries once (after delay) when validate
// rejects the content, the cashtop readRetry pattern. A missing file is not
// retried.
func readValidated(path string, delay time.Duration, validate func([]byte) error) ([]byte, error) {
	var err error
	for try := 0; try < 2; try++ {
		var data []byte
		data, err = os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, err
			}
		} else if err = validate(data); err == nil {
			return data, nil
		}
		if try == 0 {
			time.Sleep(delay)
		}
	}
	return nil, err
}

// parsePoolStatusRaw validates pool.status with poolfmt and returns the first
// three lines merged into one object with the daemon's keys and values
// untouched (numbers keep their original text).
func parsePoolStatusRaw(data []byte) (map[string]interface{}, error) {
	if _, err := poolfmt.ParsePoolStatus(bytes.NewReader(data)); err != nil {
		return nil, err
	}
	merged := map[string]interface{}{}
	n := 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if n == 3 {
			break
		}
		n++
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		var obj map[string]interface{}
		if err := dec.Decode(&obj); err != nil {
			return nil, err
		}
		for k, v := range obj {
			merged[k] = v
		}
	}
	return merged, nil
}

func validateUserFile(data []byte) error {
	if _, err := poolfmt.ParseUserFile(bytes.NewReader(data)); err != nil {
		return err
	}
	var m map[string]interface{}
	return json.Unmarshal(data, &m)
}

var errUsersUnavailable = errors.New("users snapshot unavailable")

// Outage sentinels: the wrapped detail (paths, parse errors) is logged, never
// returned to the client.
var (
	errPoolStatusUnavailable = errors.New("pool status unavailable")
	errUsersDirUnavailable   = errors.New("users directory unavailable")
)

// get returns a snapshot no older than the TTL, rebuilding when the TTL has
// elapsed or pool.status changed on disk. Callers must not mutate it.
func (c *usersSnapshotCache) get() (*usersSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	psPath := poolStatusPath()
	if c.snap != nil && c.now().Sub(c.snap.at) < c.ttl {
		if st, err := os.Stat(psPath); err == nil &&
			st.ModTime().Equal(c.snap.statMod) && st.Size() == c.snap.statLen {
			return c.snap, nil
		}
	}

	snap := &usersSnapshot{at: c.now()}

	// pool.status: missing/unparseable with no last good copy is an outage.
	poolData, err := readValidated(psPath, c.retryDelay, func(b []byte) error {
		_, e := parsePoolStatusRaw(b)
		return e
	})
	if err == nil {
		snap.pool, _ = parsePoolStatusRaw(poolData)
		c.goodPool = snap.pool
		if st, e := os.Stat(psPath); e == nil {
			snap.statMod, snap.statLen = st.ModTime(), st.Size()
		}
	} else if c.goodPool != nil {
		snap.pool = c.goodPool
	} else {
		return nil, fmt.Errorf("%w: %v", errPoolStatusUnavailable, err)
	}

	c.dirReads++
	dirEntries, err := os.ReadDir(userLogPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUsersDirUnavailable, err)
	}
	for _, de := range dirEntries {
		name := de.Name()
		if de.IsDir() || name == "" || name[0] == '.' {
			continue
		}
		info, err := de.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > usersMaxFileSize {
			snap.skipped++
			continue
		}
		data, err := readValidated(filepath.Join(userLogPath, name), c.retryDelay, validateUserFile)
		var e usersEntry
		if err == nil {
			e = usersEntry{name: name, raw: data}
			var probe struct {
				Lastshare *float64 `json:"lastshare"`
			}
			if json.Unmarshal(data, &probe) == nil && probe.Lastshare != nil {
				e.lastshare, e.hasLast = *probe.Lastshare, true
			}
			c.goodUser[name] = e
		} else if good, ok := c.goodUser[name]; ok {
			e = good
		} else {
			snap.skipped++
			continue
		}
		snap.users = append(snap.users, e)
	}
	sort.Slice(snap.users, func(i, j int) bool { return snap.users[i].name < snap.users[j].name })

	c.snap = snap
	return snap, nil
}

func writeUsersError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// handleUsers serves GET /users[?active_since=<unix>][&workers=1|true]: the
// pool totals and every user file in one response, from files the daemon
// already writes (no node RPC). Keys and values pass through as written.
// With workers on, "worker" gets the same probe filter and short-name
// annotation as /user-file; the top-level "workers" count stays the file's
// own value, so it can differ from len(worker) there.
func handleUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var activeSince float64
	filterActive := false
	if v := q.Get("active_since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeUsersError(w, http.StatusBadRequest, "active_since must be a non-negative unix timestamp")
			return
		}
		activeSince, filterActive = float64(n), true
	}
	withWorkers := false
	switch q.Get("workers") {
	case "1", "true":
		withWorkers = true
	}

	snap, err := usersCache.get()
	if err != nil {
		log.Printf("GET /users: %v", err)
		msg := errUsersDirUnavailable.Error()
		if errors.Is(err, errPoolStatusUnavailable) {
			msg = errPoolStatusUnavailable.Error()
		}
		writeUsersError(w, http.StatusServiceUnavailable, msg)
		return
	}

	users := make([]interface{}, 0, len(snap.users))
	for _, e := range snap.users {
		if filterActive && (!e.hasLast || e.lastshare < activeSince) {
			continue
		}
		var obj map[string]interface{}
		if json.Unmarshal(e.raw, &obj) != nil {
			continue
		}
		obj["user"] = e.name
		if withWorkers {
			// filterProbeWorkers also lowers "workers" for dropped probes
			// (correct for /user-file). /users keeps the file's own count
			// (D9), so restore the value it found before the filter ran.
			origWorkers, hadWorkers := obj["workers"]
			filterProbeWorkers(obj)
			if hadWorkers {
				obj["workers"] = origWorkers
			} else {
				delete(obj, "workers")
			}
			annotateUserFileWorkers(obj, e.name)
		} else {
			delete(obj, "worker")
		}
		users = append(users, obj)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"pool":      snap.pool,
		"users":     users,
		"skipped":   snap.skipped,
		"timestamp": usersCache.now().Unix(),
	})
}
