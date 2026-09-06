package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// An idle pool has a height dir with no sharelog file in it yet, so the
// bootstrap read has no file to anchor on and issues a cursor whose file
// part is empty ("<dir>||0"). That cursor is the server's own output and
// must round-trip: handing it back has to resume at the START of that dir.
//
// It previously did not. decodeCursor rejected an empty file part outright,
// so every idle poll silently downgraded to a bootstrap tail read -- and the
// moment mining resumed, a poll returned only the LAST `limit` records of
// the dir and dropped everything before them, which is precisely the gap
// /shares documents as impossible.
func TestSharesIdleCursorRoundTripsAndLosesNothing(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	// Height dir exists (ckpool created it on the new workbase) but the pool
	// has not written a share into it yet.
	const height = 300
	if err := os.MkdirAll(filepath.Join(base, heightDirName(height)), 0o755); err != nil {
		t.Fatalf("mkdir height dir: %v", err)
	}

	idle := getShares(t, map[string]string{"limit": "10"})
	if len(idle.Shares) != 0 {
		t.Fatalf("idle bootstrap shares = %d, want 0", len(idle.Shares))
	}
	if idle.NextCursor == "" {
		t.Fatal("idle poll must still issue a resumable cursor")
	}
	raw, err := base64.StdEncoding.DecodeString(idle.NextCursor)
	if err != nil {
		t.Fatalf("next_cursor is not base64: %v", err)
	}
	if _, ok := decodeCursor(idle.NextCursor); !ok {
		t.Fatalf("server issued a cursor it refuses to accept: %q (%s)", idle.NextCursor, raw)
	}

	// Mining resumes: more shares land than one poll's limit.
	lines := make([]string, 0, 5)
	for i := 1; i <= 5; i++ {
		lines = append(lines, shareLine(t, "bob", int64(i)))
	}
	writeSharelog(t, base, height, 1, lines, true)

	// Draining with limit=2 must yield 1,2 then 3,4 then 5 -- in order, once
	// each. A rejected cursor instead tail-reads and returns 4,5 first.
	var seen []int64
	cursor := idle.NextCursor
	for poll := 0; poll < 5; poll++ {
		resetShareCache(t)
		resp := getShares(t, map[string]string{"since": cursor, "limit": "2"})
		if resp.Error != "" {
			t.Fatalf("poll %d: %s", poll, resp.Error)
		}
		if len(resp.Shares) == 0 {
			break
		}
		seen = append(seen, workinfoIDs(resp.Shares)...)
		cursor = resp.NextCursor
	}

	if want := []int64{1, 2, 3, 4, 5}; !equalInt64(seen, want) {
		t.Errorf("drained workinfoids = %v, want %v (exactly once, in order)", seen, want)
	}
}
