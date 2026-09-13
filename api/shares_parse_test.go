package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// marshalShare renders a full ShareRecord as a sharelog JSON line, giving
// tests control over fields shareLine (in shares_cursor_test.go) doesn't
// expose -- sdiff, workername, a rejected share's reject-reason, etc.
func marshalShare(t *testing.T, rec ShareRecord) string {
	t.Helper()
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshaling fixture share: %v", err)
	}
	return string(b)
}

func strPtr(s string) *string { return &s }

// Well-formed lines parse and come back with the right field values --
// including a rejected share, which carries result:false plus a
// reject-reason rather than being an error case.
func TestSharesParsesWellFormedRecords(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	accepted := ShareRecord{
		WorkinfoID: 1,
		Hash:       "0000000000000000000abc123def456",
		SDiff:      123456.78,
		Workername: "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u.worker1",
		Username:   "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u",
		CreateDate: "1735689600,123456789",
		CreateInet: "127.0.0.1:3333",
		Result:     true,
	}
	rejected := ShareRecord{
		WorkinfoID:   2,
		Hash:         "0000000000000000000def456abc123",
		SDiff:        99.5,
		Workername:   "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u.worker2",
		Username:     "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u",
		CreateDate:   "1735689601,987654321",
		CreateInet:   "127.0.0.1:3333",
		Result:       false,
		RejectReason: strPtr("Above target"),
	}

	writeSharelog(t, base, 500, 1, []string{
		marshalShare(t, accepted),
		marshalShare(t, rejected),
	}, true)

	resp := getShares(t, map[string]string{"limit": "10"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
	if len(resp.Shares) != 2 {
		t.Fatalf("shares = %d, want 2", len(resp.Shares))
	}

	got := resp.Shares[0]
	switch {
	case got.Hash != accepted.Hash:
		t.Errorf("hash = %q, want %q", got.Hash, accepted.Hash)
	case got.SDiff != accepted.SDiff:
		t.Errorf("sdiff = %v, want %v", got.SDiff, accepted.SDiff)
	case got.Workername != accepted.Workername:
		t.Errorf("workername = %q, want %q", got.Workername, accepted.Workername)
	case got.Username != accepted.Username:
		t.Errorf("username = %q, want %q", got.Username, accepted.Username)
	case got.Result != true:
		t.Errorf("result = %v, want true", got.Result)
	}

	gotRejected := resp.Shares[1]
	if gotRejected.Result != false {
		t.Errorf("rejected share result = %v, want false", gotRejected.Result)
	}
	if gotRejected.RejectReason == nil || *gotRejected.RejectReason != "Above target" {
		t.Errorf("rejected share reject-reason = %v, want %q", gotRejected.RejectReason, "Above target")
	}
}

// Malformed lines (non-JSON garbage, truncated JSON, an empty line) are
// skipped -- surrounding well-formed lines still come back, and the request
// itself never errors over them.
func TestSharesSkipsMalformedLinesWithoutErroring(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	good1 := ShareRecord{WorkinfoID: 1, Username: "alice", Hash: "hash1", Result: true}
	good2 := ShareRecord{WorkinfoID: 2, Username: "alice", Hash: "hash2", Result: true}

	writeSharelog(t, base, 600, 1, []string{
		marshalShare(t, good1),
		"not json at all",
		`{"workinfoid": 3, "username": "alice", "truncated`, // truncated mid-object
		"", // empty line
		marshalShare(t, good2),
	}, true)

	resp := getShares(t, map[string]string{"limit": "10"})
	if resp.Error != "" {
		t.Fatalf("malformed lines should not error the request, got %q", resp.Error)
	}
	if got := workinfoIDs(resp.Shares); !equalInt64(got, []int64{1, 2}) {
		t.Fatalf("shares = %v, want only the well-formed [1 2]", got)
	}
}

// user filters to exactly that username, and no other username leaks
// through.
func TestSharesUserFilterExactMatch(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	writeSharelog(t, base, 700, 1, []string{
		marshalShare(t, ShareRecord{WorkinfoID: 1, Username: "alice", Result: true}),
		marshalShare(t, ShareRecord{WorkinfoID: 2, Username: "bob", Result: true}),
		marshalShare(t, ShareRecord{WorkinfoID: 3, Username: "alice", Result: true}),
	}, true)

	resp := getShares(t, map[string]string{"user": "alice", "limit": "10"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
	if got := workinfoIDs(resp.Shares); !equalInt64(got, []int64{1, 3}) {
		t.Fatalf("shares = %v, want only alice's [1 3]", got)
	}
	for _, s := range resp.Shares {
		if s.Username != "alice" {
			t.Errorf("share for username %q leaked through the user=alice filter", s.Username)
		}
	}
}

// A sharelog carrying both spellings of one CashAddr identity -- bare and
// bitcoincash:-prefixed -- must come back under EITHER query form. The pool
// names each user's stats file after the exact auth string a miner used, so
// two shares from the same key can land under different Username spellings.
func TestSharesUserFilterMatchesCashaddrSpellings(t *testing.T) {
	const bare = "qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	prefixed := "bitcoincash:" + bare

	base := useTempSharelogDir(t)
	resetShareCache(t)

	writeSharelog(t, base, 1000, 1, []string{
		marshalShare(t, ShareRecord{WorkinfoID: 1, Username: prefixed, Result: true}),
		marshalShare(t, ShareRecord{WorkinfoID: 2, Username: bare, Result: true}),
		marshalShare(t, ShareRecord{WorkinfoID: 3, Username: "someone-else", Result: true}),
	}, true)

	for _, query := range []string{bare, prefixed} {
		resetShareCache(t)
		resp := getShares(t, map[string]string{"user": query, "limit": "10"})
		if resp.Error != "" {
			t.Fatalf("query %q: unexpected error: %q", query, resp.Error)
		}
		if got := workinfoIDs(resp.Shares); !equalInt64(got, []int64{1, 2}) {
			t.Errorf("query %q: shares = %v, want both spellings' [1 2]", query, got)
		}
	}
}

// A legacy base58 username is a different string from any CashAddr spelling
// -- even one that happens to encode the same key -- and ckpool keeps the
// two accounting identities separate, so the /shares filter must never
// cross them in either direction.
func TestSharesUserFilterDoesNotCrossBase58AndCashaddr(t *testing.T) {
	const base58Addr = "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS"
	const cashaddrBare = "qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	cashaddrPrefixed := "bitcoincash:" + cashaddrBare

	base := useTempSharelogDir(t)
	resetShareCache(t)

	writeSharelog(t, base, 1100, 1, []string{
		marshalShare(t, ShareRecord{WorkinfoID: 1, Username: base58Addr, Result: true}),
		marshalShare(t, ShareRecord{WorkinfoID: 2, Username: cashaddrPrefixed, Result: true}),
	}, true)

	resp := getShares(t, map[string]string{"user": base58Addr, "limit": "10"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
	if got := workinfoIDs(resp.Shares); !equalInt64(got, []int64{1}) {
		t.Fatalf("base58 query shares = %v, want only the base58 record [1]", got)
	}

	resetShareCache(t)
	resp2 := getShares(t, map[string]string{"user": cashaddrPrefixed, "limit": "10"})
	if resp2.Error != "" {
		t.Fatalf("unexpected error: %q", resp2.Error)
	}
	if got := workinfoIDs(resp2.Shares); !equalInt64(got, []int64{2}) {
		t.Fatalf("cashaddr query shares = %v, want only the cashaddr record [2]", got)
	}
}

// The bounded dir listing (filtered to >= the cursor's own height before
// sorting) must still walk forward across MULTIPLE height dirs without loss
// or duplication, and must survive the cursor's own dir being pruned out
// from under it -- resuming at the oldest surviving dir at or above the
// cursor's height, same guarantee shares_cursor_test.go proves for a single
// rollover, now exercised across a longer chain.
func TestSharesBoundedScanCrossesMultipleHeightDirsAndSurvivesPruning(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	writeSharelog(t, base, 1200, 1, []string{
		shareLine(t, "erin", 1),
		shareLine(t, "erin", 2),
	}, true)

	first := getShares(t, map[string]string{"limit": "10"})
	if first.Error != "" {
		t.Fatalf("bootstrap error: %q", first.Error)
	}
	if got := workinfoIDs(first.Shares); !equalInt64(got, []int64{1, 2}) {
		t.Fatalf("bootstrap shares = %v, want [1 2]", got)
	}

	// Two more height dirs land before the next poll -- the bounded listing
	// (floored at the first cursor's height) must still reach both.
	writeSharelog(t, base, 1201, 1, []string{
		shareLine(t, "erin", 3),
	}, true)
	writeSharelog(t, base, 1202, 1, []string{
		shareLine(t, "erin", 4),
		shareLine(t, "erin", 5),
	}, true)

	// The cursor's own height dir is pruned out from under it, same as
	// clean-old-blocks.sh would do -- resolution must fall through to the
	// oldest surviving dir at or above the cursor's height (1201), not error
	// or silently skip past records that were never seen.
	if err := os.RemoveAll(filepath.Join(base, heightDirName(1200))); err != nil {
		t.Fatalf("pruning height dir: %v", err)
	}

	second := getShares(t, map[string]string{"since": first.NextCursor, "limit": "10"})
	if second.Error != "" {
		t.Fatalf("post-prune poll error: %q", second.Error)
	}
	if got := workinfoIDs(second.Shares); !equalInt64(got, []int64{3, 4, 5}) {
		t.Fatalf("post-prune shares = %v, want [3 4 5] with none dropped or duplicated", got)
	}
	if second.Height == nil || *second.Height != 1202 {
		t.Fatalf("height = %v, want 1202 (the latest dir)", second.Height)
	}

	third := getShares(t, map[string]string{"since": second.NextCursor, "limit": "10"})
	if third.Error != "" {
		t.Fatalf("third poll error: %q", third.Error)
	}
	if len(third.Shares) != 0 {
		t.Fatalf("third poll shares = %d, want 0 (nothing new)", len(third.Shares))
	}
	if third.NextCursor != second.NextCursor {
		t.Errorf("cursor moved on an empty poll: %q -> %q", second.NextCursor, third.NextCursor)
	}
}

// limit is clamped to [1, MaxLines] regardless of what the caller asks for.
func TestSharesLimitClamping(t *testing.T) {
	t.Run("zero or negative clamps to 1", func(t *testing.T) {
		base := useTempSharelogDir(t)
		resetShareCache(t)

		writeSharelog(t, base, 800, 1, []string{
			marshalShare(t, ShareRecord{WorkinfoID: 1, Username: "carol", Result: true}),
			marshalShare(t, ShareRecord{WorkinfoID: 2, Username: "carol", Result: true}),
			marshalShare(t, ShareRecord{WorkinfoID: 3, Username: "carol", Result: true}),
		}, true)

		for _, limitParam := range []string{"0", "-5"} {
			resp := getShares(t, map[string]string{"limit": limitParam})
			if len(resp.Shares) != 1 {
				t.Errorf("limit=%s: shares = %d, want 1 (clamped up from below 1)", limitParam, len(resp.Shares))
				continue
			}
			if got := resp.Shares[0].WorkinfoID; got != 3 {
				t.Errorf("limit=%s: kept workinfoid %d, want the most recent (3)", limitParam, got)
			}
		}
	})

	t.Run("above MaxLines clamps to MaxLines", func(t *testing.T) {
		base := useTempSharelogDir(t)
		resetShareCache(t)

		// Just over MaxLines is enough to prove the clamp fires -- no need
		// to actually write as many lines as the literal 5000 limit param
		// requests.
		total := MaxLines + 5
		lines := make([]string, total)
		for i := 0; i < total; i++ {
			lines[i] = marshalShare(t, ShareRecord{WorkinfoID: int64(i + 1), Username: "dave", Result: true})
		}
		writeSharelog(t, base, 900, 1, lines, true)

		resp := getShares(t, map[string]string{"limit": "5000"})
		if resp.Error != "" {
			t.Fatalf("unexpected error: %q", resp.Error)
		}
		if len(resp.Shares) != MaxLines {
			t.Fatalf("shares = %d, want %d (clamped from the requested 5000)", len(resp.Shares), MaxLines)
		}

		got := workinfoIDs(resp.Shares)
		wantFirst, wantLast := int64(total-MaxLines+1), int64(total)
		if got[0] != wantFirst || got[len(got)-1] != wantLast {
			t.Errorf("kept range = [%d..%d], want the most recent %d records ([%d..%d])",
				got[0], got[len(got)-1], MaxLines, wantFirst, wantLast)
		}
	})
}

// version_mask is the BIP310 mask the miner submitted, appended to the
// sharelog record by ckpool. It reaches /shares verbatim, and a line written
// before that field existed decodes as "" rather than failing to parse -- the
// property that lets the API read a mixed-age sharelog. Both cases are asserted
// from a RAW sharelog line rather than a marshalled ShareRecord, so the test
// still means something if the struct tag is ever renamed.
func TestSharesSurfacesVersionMask(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	const withMask = `{"workinfoid":1,"clientid":1,"enonce1":"deadbeef",` +
		`"nonce2":"0000000000000000","nonce":"11223344","ntime":"6a9b65bc",` +
		`"diff":1.0,"sdiff":2.5,"hash":"abc","result":true,"error":"Valid",` +
		`"errn":0,"createdate":"1735689600,1","createby":"code",` +
		`"createcode":"parse_submit","createinet":"127.0.0.1:3333",` +
		`"workername":"alice.w1","username":"alice","address":"127.0.0.1",` +
		`"agent":"cpuminer/2.5.1","version_mask":"1fffe000"}`
	// Exactly the same record as written before ckpool learned the field.
	const withoutMask = `{"workinfoid":2,"clientid":1,"enonce1":"deadbeef",` +
		`"nonce2":"0000000000000000","nonce":"55667788","ntime":"6a9b65bd",` +
		`"diff":1.0,"sdiff":3.5,"hash":"def","result":true,"error":"Valid",` +
		`"errn":0,"createdate":"1735689601,1","createby":"code",` +
		`"createcode":"parse_submit","createinet":"127.0.0.1:3333",` +
		`"workername":"alice.w1","username":"alice","address":"127.0.0.1",` +
		`"agent":"cpuminer/2.5.1"}`

	writeSharelog(t, base, 500, 1, []string{withMask, withoutMask}, true)

	resp := getShares(t, map[string]string{"limit": "10"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
	if len(resp.Shares) != 2 {
		t.Fatalf("want 2 shares, got %d", len(resp.Shares))
	}

	got := map[int64]string{}
	for _, s := range resp.Shares {
		got[s.WorkinfoID] = s.VersionMask
	}
	if got[1] != "1fffe000" {
		t.Errorf("workinfoid 1: version_mask = %q, want %q", got[1], "1fffe000")
	}
	if got[2] != "" {
		t.Errorf("workinfoid 2 (pre-field line): version_mask = %q, want empty", got[2])
	}

	// The field must also survive re-encoding, since /shares marshals the
	// struct back out rather than echoing the input line.
	b, err := json.Marshal(resp.Shares[0])
	if err != nil {
		t.Fatalf("re-marshaling share: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshaling re-encoded share: %v", err)
	}
	if _, ok := round["version_mask"]; !ok {
		t.Errorf("re-encoded share has no version_mask key: %s", b)
	}
}
