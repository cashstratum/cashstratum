package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempSharelogDir points logPath's directory at a fresh temp dir for one
// test and restores logPath afterwards. handleShares derives the sharelog
// base dir from filepath.Dir(logPath) -- the same global the other handlers
// read logPath itself from.
func useTempSharelogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := logPath
	logPath = filepath.Join(dir, "ckpool.log")
	t.Cleanup(func() { logPath = orig })
	return dir
}

// resetShareCache isolates /shares' response cache per test. The cache key
// does not embed the sharelog base dir (there is only ever one in a real
// deployment), so two tests reusing the same since/user/limit combination
// would otherwise see each other's cached response inside the 2s freshness
// window.
func resetShareCache(t *testing.T) {
	t.Helper()
	orig := responseCache
	responseCache = NewLRUCache(CacheSize)
	t.Cleanup(func() { responseCache = orig })
}

func heightDirName(height int) string {
	return fmt.Sprintf("%08x", height)
}

// writeSharelog writes one *.sharelog file under base/<height dir>, named
// after workinfoid the way ckpool names them. terminated controls whether
// the final line gets its trailing newline -- false simulates ckpool's
// O_APPEND writer caught mid-write.
func writeSharelog(t *testing.T, base string, height int, workinfoid uint64, lines []string, terminated bool) string {
	t.Helper()
	dir := filepath.Join(base, heightDirName(height))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	name := fmt.Sprintf("%016x.sharelog", workinfoid)
	path := filepath.Join(dir, name)
	content := strings.Join(lines, "\n")
	if len(lines) > 0 && terminated {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing sharelog %q: %v", path, err)
	}
	return path
}

// appendToSharelog appends raw bytes to an existing sharelog file, as
// ckpool's O_APPEND writer would.
func appendToSharelog(t *testing.T, path string, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening %q for append: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatalf("appending to %q: %v", path, err)
	}
}

// shareLine renders one ShareRecord as a sharelog JSON line.
func shareLine(t *testing.T, username string, workinfoid int64) string {
	t.Helper()
	rec := ShareRecord{
		WorkinfoID: workinfoid,
		Username:   username,
		Hash:       fmt.Sprintf("hash-%d", workinfoid),
		Result:     true,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshaling fixture share: %v", err)
	}
	return string(b)
}

// getShares issues a /shares request built from params (URL-encoded properly
// -- a raw base64 cursor can contain '+' or '/', which must not be hand
// concatenated into a query string or url.Values.Encode's escaping would be
// bypassed and '+' would silently decode back as a space).
func getShares(t *testing.T, params map[string]string) SharesResponse {
	t.Helper()
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodGet, "/shares?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	handleShares(rec, req)
	var resp SharesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return resp
}

func workinfoIDs(shares []ShareRecord) []int64 {
	ids := make([]int64, len(shares))
	for i, s := range shares {
		ids[i] = s.WorkinfoID
	}
	return ids
}

func equalInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// (a) Two sequential polls, the second using the first's next_cursor, must
// never return the same workinfoid twice.
func TestSharesSequentialPollsNoDuplicates(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	path := writeSharelog(t, base, 100, 1, []string{
		shareLine(t, "alice", 1),
		shareLine(t, "alice", 2),
		shareLine(t, "alice", 3),
		shareLine(t, "alice", 4),
		shareLine(t, "alice", 5),
	}, true)

	first := getShares(t, map[string]string{"limit": "3"})
	if first.Error != "" {
		t.Fatalf("first poll error: %q", first.Error)
	}
	if got := workinfoIDs(first.Shares); !equalInt64(got, []int64{3, 4, 5}) {
		t.Fatalf("first poll shares = %v, want most-recent-3 [3 4 5]", got)
	}
	if first.NextCursor == "" {
		t.Fatal("first poll: next_cursor should be set")
	}

	// New shares land after the first poll.
	appendToSharelog(t, path, shareLine(t, "alice", 6)+"\n"+shareLine(t, "alice", 7)+"\n")

	second := getShares(t, map[string]string{"since": first.NextCursor, "limit": "10"})
	if second.Error != "" {
		t.Fatalf("second poll error: %q", second.Error)
	}
	if got := workinfoIDs(second.Shares); !equalInt64(got, []int64{6, 7}) {
		t.Fatalf("second poll shares = %v, want only the new [6 7]", got)
	}

	seen := map[int64]bool{}
	for _, id := range append(workinfoIDs(first.Shares), workinfoIDs(second.Shares)...) {
		if seen[id] {
			t.Fatalf("workinfoid %d returned across both polls", id)
		}
		seen[id] = true
	}
}

// (b) A poll that finds nothing new must echo the same cursor back, not
// advance it and not error.
func TestSharesEmptyPollLeavesCursorUnchanged(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	writeSharelog(t, base, 200, 1, []string{
		shareLine(t, "bob", 1),
		shareLine(t, "bob", 2),
	}, true)

	first := getShares(t, map[string]string{"limit": "10"})
	if len(first.Shares) != 2 {
		t.Fatalf("bootstrap shares = %d, want 2", len(first.Shares))
	}

	second := getShares(t, map[string]string{"since": first.NextCursor, "limit": "10"})
	if len(second.Shares) != 0 {
		t.Fatalf("empty poll shares = %d, want 0", len(second.Shares))
	}
	if second.Error != "" {
		t.Fatalf("empty poll should not be an error, got %q", second.Error)
	}
	if second.NextCursor != first.NextCursor {
		t.Errorf("cursor moved on an empty poll: %q -> %q", first.NextCursor, second.NextCursor)
	}
}

// (c) A cursor issued at height H must still resolve, without loss or
// duplication, once the pool rolls over to a new height dir H+1 -- the old
// dir is left in place (clean-old-blocks.sh prunes it later, separately).
func TestSharesSurvivesRollover(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	writeSharelog(t, base, 300, 1, []string{
		shareLine(t, "carol", 1),
		shareLine(t, "carol", 2),
	}, true)

	first := getShares(t, map[string]string{"limit": "10"})
	if got := workinfoIDs(first.Shares); !equalInt64(got, []int64{1, 2}) {
		t.Fatalf("pre-rollover shares = %v, want [1 2]", got)
	}
	if first.Height == nil || *first.Height != 300 {
		t.Fatalf("height = %v, want 300", first.Height)
	}

	// Rollover: a new, higher height dir appears.
	writeSharelog(t, base, 301, 1, []string{
		shareLine(t, "carol", 3),
		shareLine(t, "carol", 4),
	}, true)

	second := getShares(t, map[string]string{"since": first.NextCursor, "limit": "10"})
	if second.Error != "" {
		t.Fatalf("post-rollover poll error: %q", second.Error)
	}
	if got := workinfoIDs(second.Shares); !equalInt64(got, []int64{3, 4}) {
		t.Fatalf("post-rollover shares = %v, want only the new block's [3 4]", got)
	}
	if second.Height == nil || *second.Height != 301 {
		t.Fatalf("height after rollover = %v, want 301", second.Height)
	}
}

// (d) A final line with no trailing newline (a write in progress) must not
// be consumed; it must come back whole once its newline arrives.
func TestSharesTornLineNotConsumed(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	complete := shareLine(t, "dave", 1)
	torn := shareLine(t, "dave", 2) // written below with NO trailing newline

	dir := filepath.Join(base, heightDirName(400))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "0000000000000001.sharelog")
	if err := os.WriteFile(path, []byte(complete+"\n"+torn), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	first := getShares(t, map[string]string{"limit": "10"})
	if first.Error != "" {
		t.Fatalf("first poll error: %q", first.Error)
	}
	if got := workinfoIDs(first.Shares); !equalInt64(got, []int64{1}) {
		t.Fatalf("first poll shares = %v, want only the complete line [1]", got)
	}

	// ckpool finishes the write: the torn line finally gets its newline.
	appendToSharelog(t, path, "\n")

	second := getShares(t, map[string]string{"since": first.NextCursor, "limit": "10"})
	if second.Error != "" {
		t.Fatalf("second poll error: %q", second.Error)
	}
	if got := workinfoIDs(second.Shares); !equalInt64(got, []int64{2}) {
		t.Fatalf("second poll shares = %v, want the now-complete line [2]", got)
	}
}
