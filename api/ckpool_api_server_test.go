package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cashstratumapi/internal/ckconf"
)

func useIsolatedCoinbaseState(t *testing.T) {
	t.Helper()
	originalCache := responseCache
	originalFlights := coinbaseFlights
	responseCache = NewLRUCache(CacheSize)
	coinbaseFlights = NewCoinbaseFlightGroup()
	t.Cleanup(func() {
		responseCache = originalCache
		coinbaseFlights = originalFlights
	})
}

func candidateHeight(height int) *int {
	return &height
}

func successfulCoinbaseResult(username string, height int, marker string) coinbaseFetchResult {
	return coinbaseFetchResult{
		status: http.StatusOK,
		response: CoinbaseResponse{
			Username:        username,
			BlockHeight:     candidateHeight(height),
			CoinbaseMessage: marker,
			Timestamp:       time.Now().Unix(),
		},
	}
}

func TestLRUCacheUpdatesOrderWithoutGrowingPastCapacity(t *testing.T) {
	cache := NewLRUCache(2)
	cache.Set("a", 1)
	cache.Set("b", 2)

	// Repeated updates must not duplicate the key in the order slice.
	cache.Set("a", 10)
	cache.Set("a", 11)
	cache.Set("c", 3)

	if _, found := cache.Get("b"); found {
		t.Error("b should be evicted after a was refreshed")
	}
	if got, found := cache.Get("a"); !found || got != 11 {
		t.Fatalf("a = %v, found=%v; want updated value 11", got, found)
	}
	if len(cache.cache) != 2 || len(cache.order) != 2 {
		t.Fatalf("cache/order sizes = %d/%d, want 2/2", len(cache.cache), len(cache.order))
	}

	// Get is an LRU access: touching a makes c the next eviction victim.
	cache.Set("d", 4)
	if _, found := cache.Get("c"); found {
		t.Error("c should be evicted after a was read most recently")
	}
	if _, found := cache.Get("a"); !found {
		t.Error("a should remain after its recent access")
	}
}

// useTempUserLogDir points userLogPath at a fresh temp dir for one test and
// restores it afterwards.
func useTempUserLogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := userLogPath
	userLogPath = dir
	t.Cleanup(func() { userLogPath = orig })
	return dir
}

func writeUserFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"shares":1}`), 0o644); err != nil {
		t.Fatalf("writing fixture %q: %v", name, err)
	}
}

func TestResolveUserFile(t *testing.T) {
	const bare = "qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"

	tests := []struct {
		name         string
		files        []string
		query        string
		wantFound    bool
		wantResolved string
	}{
		{
			name:         "exact prefixed hit",
			files:        []string{"bitcoincash:" + bare},
			query:        "bitcoincash:" + bare,
			wantFound:    true,
			wantResolved: "bitcoincash:" + bare,
		},
		{
			name:         "exact bare hit",
			files:        []string{bare},
			query:        bare,
			wantFound:    true,
			wantResolved: bare,
		},
		{
			name:         "prefixed query falls back to bare file",
			files:        []string{bare},
			query:        "bitcoincash:" + bare,
			wantFound:    true,
			wantResolved: bare,
		},
		{
			name:         "bare query falls back to prefixed file",
			files:        []string{"bitcoincash:" + bare},
			query:        bare,
			wantFound:    true,
			wantResolved: "bitcoincash:" + bare,
		},
		{
			name:         "bare query reaches testnet prefix",
			files:        []string{"bchtest:" + bare},
			query:        bare,
			wantFound:    true,
			wantResolved: "bchtest:" + bare,
		},
		{
			name:         "exact beats fallback when both exist (prefixed query)",
			files:        []string{"bitcoincash:" + bare, bare},
			query:        "bitcoincash:" + bare,
			wantFound:    true,
			wantResolved: "bitcoincash:" + bare,
		},
		{
			name:         "exact beats fallback when both exist (bare query)",
			files:        []string{"bitcoincash:" + bare, bare},
			query:        bare,
			wantFound:    true,
			wantResolved: bare,
		},
		{
			name:      "miss",
			files:     nil,
			query:     "bitcoincash:" + bare,
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := useTempUserLogDir(t)
			for _, f := range tt.files {
				writeUserFile(t, dir, f)
			}
			path, resolved, found := resolveUserFile(tt.query)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if !found {
				return
			}
			if resolved != tt.wantResolved {
				t.Errorf("resolved = %q, want %q", resolved, tt.wantResolved)
			}
			if want := filepath.Join(dir, tt.wantResolved); path != want {
				t.Errorf("path = %q, want %q", path, want)
			}
		})
	}
}

func TestResolveUserFileCannotEscapeUsersDir(t *testing.T) {
	dir := useTempUserLogDir(t)

	// A real file OUTSIDE the users dir that traversal would reach if
	// sanitization ever regressed.
	outside := filepath.Join(filepath.Dir(dir), "passwd")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("writing outside fixture: %v", err)
	}

	for _, query := range []string{
		"../passwd",
		"../../etc/passwd",
		"bitcoincash:../passwd",
		"bchreg:../../passwd",
		"sub/../../passwd",
	} {
		if path, _, found := resolveUserFile(query); found {
			t.Errorf("query %q escaped the users dir: resolved to %q", query, path)
		}
	}

	// filepath.Base collapses a traversal to its last element, so the SAME
	// basename inside the dir must still resolve — proving containment, not
	// blanket rejection.
	writeUserFile(t, dir, "passwd")
	if _, resolved, found := resolveUserFile("../passwd"); !found || resolved != "passwd" {
		t.Errorf("in-dir basename should resolve after sanitization; found=%v resolved=%q", found, resolved)
	}
}

func TestCanonicalAuthUser(t *testing.T) {
	const bare = "qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	upperBare := strings.ToUpper(bare)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "prefixed lowercase is unchanged",
			input: "bitcoincash:" + bare,
			want:  "bitcoincash:" + bare,
		},
		{
			name:  "bare lowercase is prefixed",
			input: bare,
			want:  "bitcoincash:" + bare,
		},
		{
			name:  "uppercase prefix and payload are lowercased, prefix kept as-is",
			input: "BITCOINCASH:" + upperBare,
			want:  "bitcoincash:" + bare,
		},
		{
			name:  "bare uppercase is lowercased and prefixed",
			input: upperBare,
			want:  "bitcoincash:" + bare,
		},
		{
			name:  "legacy base58 address is unchanged, case preserved",
			input: "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS",
			want:  "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS",
		},
		{
			name:  "plain non-address name is unchanged",
			input: "someminer",
			want:  "someminer",
		},
		{
			name:  "dot worker suffix is dropped from a bare CashAddr",
			input: bare + ".rig1",
			want:  "bitcoincash:" + bare,
		},
		{
			name:  "underscore worker suffix is dropped from a prefixed CashAddr",
			input: "bitcoincash:" + bare + "_worker2",
			want:  "bitcoincash:" + bare,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canonicalAuthUser(tt.input); got != tt.want {
				t.Errorf("canonicalAuthUser(%q) = %q, want %q", tt.input, got, tt.want)
			}
			// This is exactly how fetchCoinbaseFromStratum builds the probe's
			// auth string -- pin the full construction, not just the helper.
			wantAuthUser := tt.want + "." + ProbeWorkerSuffix
			if got := canonicalAuthUser(tt.input) + "." + ProbeWorkerSuffix; got != wantAuthUser {
				t.Errorf("authUser = %q, want %q", got, wantAuthUser)
			}
		})
	}
}

func TestCanonicalAuthUserRespectsConfiguredPrefix(t *testing.T) {
	const bare = "qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"

	original := cashaddrPrefix
	cashaddrPrefix = "bchreg"
	defer func() { cashaddrPrefix = original }()

	if got, want := canonicalAuthUser(bare), "bchreg:"+bare; got != want {
		t.Errorf("canonicalAuthUser(%q) = %q, want %q", bare, got, want)
	}
	// An already-prefixed address keeps its own prefix -- cashaddrPrefix only
	// applies to a bare address, never rewrites an explicit one.
	if got, want := canonicalAuthUser("bitcoincash:"+bare), "bitcoincash:"+bare; got != want {
		t.Errorf("canonicalAuthUser(%q) = %q, want %q", "bitcoincash:"+bare, got, want)
	}
}

func TestHandleUserFileFallback(t *testing.T) {
	dir := useTempUserLogDir(t)
	const bare = "qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	writeUserFile(t, dir, "bitcoincash:"+bare)

	get := func(user string) UserFileResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/user-file?user="+user, nil)
		rec := httptest.NewRecorder()
		handleUserFile(rec, req)
		var resp UserFileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		return resp
	}

	// Bare query resolves the prefixed file and reports the canonical name.
	resp := get(bare)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
	if resp.Username != "bitcoincash:"+bare {
		t.Errorf("username = %q, want canonical %q", resp.Username, "bitcoincash:"+bare)
	}
	if resp.Content == nil {
		t.Error("content should be populated")
	}

	// Unknown user still reports not found.
	if resp := get("qqnope"); resp.Error != "User not found" {
		t.Errorf("miss: error = %q, want %q", resp.Error, "User not found")
	}
}

// filterProbeWorkers must drop exactly the /coinbase probe entry, adjust
// "workers" by exactly the number dropped (never recomputed from the array's
// new length), and leave every other field untouched.
func TestFilterProbeWorkersDropsProbeAndAdjustsCount(t *testing.T) {
	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	raw := `{
		"shares": 100,
		"workers": 2,
		"worker": [
			{"workername": "` + addr + `.nh", "shares": 90},
			{"workername": "` + addr + `.` + ProbeWorkerSuffix + `", "shares": 10}
		]
	}`
	var content interface{}
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	filterProbeWorkers(content)

	obj := content.(map[string]interface{})
	arr, ok := obj["worker"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Fatalf("worker array = %v, want exactly 1 entry", obj["worker"])
	}
	entry := arr[0].(map[string]interface{})
	if entry["workername"] != addr+".nh" {
		t.Errorf("remaining entry = %v, want the non-probe worker", entry)
	}
	if got := obj["workers"]; got != float64(1) {
		t.Errorf("workers = %v, want 1 (2 - 1 dropped)", got)
	}
	if obj["shares"] != float64(100) {
		t.Errorf("shares = %v, want untouched 100", obj["shares"])
	}
}

// ckpool's "workers" is its own independent decay bookkeeping and is
// routinely 0 while a probe worker is still listed -- the subtraction must
// floor at 0, never go negative.
func TestFilterProbeWorkersNeverGoesNegative(t *testing.T) {
	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	raw := `{
		"workers": 0,
		"worker": [
			{"workername": "` + addr + `.` + ProbeWorkerSuffix + `"}
		]
	}`
	var content interface{}
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	filterProbeWorkers(content)
	obj := content.(map[string]interface{})
	if got := obj["workers"]; got != float64(0) {
		t.Errorf("workers = %v, want floored at 0, not negative", got)
	}
}

// Dual-filter: both .cashstratum-api (ProbeWorkerSuffix) and legacy .ckpool-api
// probe workers must be filtered out.
func TestFilterProbeWorkersDualFilter(t *testing.T) {
	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	raw := `{
		"shares": 100,
		"workers": 3,
		"worker": [
			{"workername": "` + addr + `.nh", "shares": 70},
			{"workername": "` + addr + `.cashstratum-api", "shares": 10},
			{"workername": "` + addr + `.ckpool-api", "shares": 20}
		]
	}`
	var content interface{}
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	filterProbeWorkers(content)

	obj := content.(map[string]interface{})
	arr, ok := obj["worker"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Fatalf("worker array = %v, want exactly 1 entry", obj["worker"])
	}
	entry := arr[0].(map[string]interface{})
	if entry["workername"] != addr+".nh" {
		t.Errorf("remaining entry = %v, want the non-probe worker", entry)
	}
	if got := obj["workers"]; got != float64(1) {
		t.Errorf("workers = %v, want 1 (3 - 2 dropped)", got)
	}
}

// A file with no probe worker passes through byte-for-byte.
func TestFilterProbeWorkersPassesThroughWhenNoProbe(t *testing.T) {
	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	raw := `{"workers":1,"worker":[{"workername":"` + addr + `.nh"}]}`
	var content interface{}
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	filterProbeWorkers(content)

	var want interface{}
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	gotJSON, _ := json.Marshal(content)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("content = %s, want unchanged %s", gotJSON, wantJSON)
	}
}

// End-to-end through the handler: a user file containing a probe worker
// comes back filtered, "workers" reduced by exactly one, every other field
// untouched.
func TestHandleUserFileFiltersProbeWorker(t *testing.T) {
	dir := useTempUserLogDir(t)
	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"

	body := `{
		"shares": 145146117994,
		"luck": 32.29,
		"worker": [
			{"workername": "` + addr + `.nh", "shares": 145146117994},
			{"workername": "` + addr + `.` + ProbeWorkerSuffix + `", "shares": 1}
		],
		"workers": 2
	}`
	if err := os.WriteFile(filepath.Join(dir, addr), []byte(body), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/user-file?user="+addr, nil)
	rec := httptest.NewRecorder()
	handleUserFile(rec, req)

	var resp struct {
		Content struct {
			Shares  float64 `json:"shares"`
			Luck    float64 `json:"luck"`
			Workers float64 `json:"workers"`
			Worker  []struct {
				Workername string `json:"workername"`
			} `json:"worker"`
		} `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(resp.Content.Worker) != 1 {
		t.Fatalf("worker entries = %d, want 1 (probe filtered out)", len(resp.Content.Worker))
	}
	if resp.Content.Worker[0].Workername != addr+".nh" {
		t.Errorf("remaining worker = %q, want the non-probe one", resp.Content.Worker[0].Workername)
	}
	if resp.Content.Workers != 1 {
		t.Errorf("workers = %v, want 2 - 1 dropped = 1", resp.Content.Workers)
	}
	if resp.Content.Shares != 145146117994 {
		t.Errorf("shares = %v, want untouched 145146117994", resp.Content.Shares)
	}
	if resp.Content.Luck != 32.29 {
		t.Errorf("luck = %v, want untouched 32.29", resp.Content.Luck)
	}
}

// A user file with no probe worker passes through the handler byte-for-byte
// (module the pre-existing additive "worker" short-name annotation).
func TestHandleUserFileNoProbePassesThroughUntouched(t *testing.T) {
	dir := useTempUserLogDir(t)
	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"

	body := `{"shares":42,"workers":1,"worker":[{"workername":"` + addr + `.nh","shares":42}]}`
	if err := os.WriteFile(filepath.Join(dir, addr), []byte(body), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/user-file?user="+addr, nil)
	rec := httptest.NewRecorder()
	handleUserFile(rec, req)

	var resp UserFileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	content, ok := resp.Content.(map[string]interface{})
	if !ok {
		t.Fatalf("content = %#v, want a JSON object", resp.Content)
	}
	if content["workers"] != float64(1) {
		t.Errorf("workers = %v, want untouched 1", content["workers"])
	}
	arr, ok := content["worker"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Fatalf("worker array = %v, want exactly 1 entry (nothing filtered)", content["worker"])
	}
}

func TestCoinbaseConcurrentMissesShareOneProbe(t *testing.T) {
	useIsolatedCoinbaseState(t)

	const (
		username    = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
		height      = 966500
		callerCount = 32
	)

	start := make(chan struct{})
	releaseProbe := make(chan struct{})
	probeStarted := make(chan struct{})
	var startOnce sync.Once
	var probeCount atomic.Int32

	fetch := func(gotUsername string) coinbaseFetchResult {
		count := probeCount.Add(1)
		startOnce.Do(func() { close(probeStarted) })
		<-releaseProbe
		return successfulCoinbaseResult(gotUsername, height, fmt.Sprintf("probe-%d", count))
	}

	results := make(chan coinbaseFetchResult, callerCount)
	errors := make(chan error, callerCount)
	var callers sync.WaitGroup
	callers.Add(callerCount)
	for range callerCount {
		go func() {
			defer callers.Done()
			<-start
			result, err := getCoinbaseProof(context.Background(), username, candidateHeight(height), fetch)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}()
	}

	close(start)
	<-probeStarted
	// Keep the owner blocked long enough for the other ready goroutines to
	// encounter the in-flight call. The release channel is closed even on a
	// failure so a regression cannot strand the test goroutines.
	time.Sleep(25 * time.Millisecond)
	countWhileBlocked := probeCount.Load()
	close(releaseProbe)
	callers.Wait()
	close(results)
	close(errors)

	if countWhileBlocked != 1 {
		t.Fatalf("concurrent cache misses opened %d probes while blocked, want 1", countWhileBlocked)
	}
	if len(errors) != 0 {
		t.Fatalf("concurrent callers returned %d errors", len(errors))
	}
	if got := probeCount.Load(); got != 1 {
		t.Fatalf("probe count = %d, want 1", got)
	}
	if got := len(results); got != callerCount {
		t.Fatalf("results = %d, want %d", got, callerCount)
	}
	for result := range results {
		if result.response.CoinbaseMessage != "probe-1" {
			t.Errorf("shared result marker = %q, want probe-1", result.response.CoinbaseMessage)
		}
	}
}

func TestCoinbaseDifferentCandidateHeightsQueueWithoutSharingProof(t *testing.T) {
	useIsolatedCoinbaseState(t)

	const username = "miner"
	firstProbeStarted := make(chan struct{})
	releaseFirstProbe := make(chan struct{})
	var probeCount atomic.Int32
	var activeProbes atomic.Int32
	var maxActiveProbes atomic.Int32

	fetch := func(gotUsername string) coinbaseFetchResult {
		probe := probeCount.Add(1)
		active := activeProbes.Add(1)
		for {
			maximum := maxActiveProbes.Load()
			if active <= maximum || maxActiveProbes.CompareAndSwap(maximum, active) {
				break
			}
		}
		defer activeProbes.Add(-1)

		if probe == 1 {
			close(firstProbeStarted)
			<-releaseFirstProbe
		}

		return successfulCoinbaseResult(gotUsername, 966499+int(probe), fmt.Sprintf("probe-%d", probe))
	}

	firstResult := make(chan coinbaseFetchResult, 1)
	secondResult := make(chan coinbaseFetchResult, 1)
	go func() {
		result, _ := getCoinbaseProof(context.Background(), username, candidateHeight(966500), fetch)
		firstResult <- result
	}()
	<-firstProbeStarted

	go func() {
		result, _ := getCoinbaseProof(context.Background(), username, candidateHeight(966501), fetch)
		secondResult <- result
	}()

	time.Sleep(25 * time.Millisecond)
	if got := probeCount.Load(); got != 1 {
		t.Fatalf("different height opened a simultaneous probe; count = %d", got)
	}
	close(releaseFirstProbe)

	first := <-firstResult
	second := <-secondResult
	if got := *first.response.BlockHeight; got != 966500 {
		t.Errorf("first height = %d, want 966500", got)
	}
	if got := *second.response.BlockHeight; got != 966501 {
		t.Errorf("second height = %d, want 966501", got)
	}
	if first.response.CoinbaseMessage == second.response.CoinbaseMessage {
		t.Errorf("different candidate heights shared %q", first.response.CoinbaseMessage)
	}
	if got := probeCount.Load(); got != 2 {
		t.Errorf("probe count = %d, want one serialized probe per height", got)
	}
	if got := maxActiveProbes.Load(); got != 1 {
		t.Errorf("maximum simultaneous probes = %d, want 1", got)
	}
}

func TestCoinbaseFlightCleansUpAfterPanic(t *testing.T) {
	group := NewCoinbaseFlightGroup()

	func() {
		defer func() {
			if recovered := recover(); recovered != "probe panic" {
				t.Fatalf("recovered = %v, want probe panic", recovered)
			}
		}()

		_, _ = group.Do(context.Background(), "miner", "height:966500", func() coinbaseFetchResult {
			panic("probe panic")
		})
	}()

	group.mu.Lock()
	remaining := len(group.calls)
	group.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("in-flight calls after panic = %d, want 0", remaining)
	}

	result, err := group.Do(context.Background(), "miner", "height:966501", func() coinbaseFetchResult {
		return successfulCoinbaseResult("miner", 966501, "recovered")
	})
	if err != nil {
		t.Fatalf("call after panic: %v", err)
	}
	if result.response.CoinbaseMessage != "recovered" {
		t.Errorf("call after panic = %q", result.response.CoinbaseMessage)
	}
}

func TestCoinbaseCandidateCachePersistsUntilHeightChanges(t *testing.T) {
	useIsolatedCoinbaseState(t)

	const username = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	var probeCount atomic.Int32
	currentHeight := 966500
	fetch := func(gotUsername string) coinbaseFetchResult {
		count := probeCount.Add(1)
		return successfulCoinbaseResult(gotUsername, currentHeight, fmt.Sprintf("probe-%d", count))
	}

	first, err := getCoinbaseProof(context.Background(), username, candidateHeight(currentHeight), fetch)
	if err != nil {
		t.Fatalf("first proof: %v", err)
	}

	// Candidate-scoped reuse is not a disguised TTL. Age both the response
	// and underlying LRU entry far beyond CacheTTL and verify the explicit
	// height still owns invalidation.
	first.response.Timestamp = time.Now().Add(-24 * time.Hour).Unix()
	key := coinbaseCandidateCacheKey(username)
	responseCache.mu.Lock()
	responseCache.cache[key].data = first.response
	responseCache.cache[key].timestamp = time.Now().Add(-24 * time.Hour)
	responseCache.mu.Unlock()

	reused, err := getCoinbaseProof(context.Background(), username, candidateHeight(currentHeight), fetch)
	if err != nil {
		t.Fatalf("reused proof: %v", err)
	}
	if reused.response.CoinbaseMessage != "probe-1" {
		t.Errorf("same-height proof = %q, want cached probe-1", reused.response.CoinbaseMessage)
	}
	if got := probeCount.Load(); got != 1 {
		t.Fatalf("same candidate opened %d probes, want 1", got)
	}

	currentHeight++
	refreshed, err := getCoinbaseProof(context.Background(), username, candidateHeight(currentHeight), fetch)
	if err != nil {
		t.Fatalf("new-height proof: %v", err)
	}
	if refreshed.response.CoinbaseMessage != "probe-2" {
		t.Errorf("new-height proof = %q, want probe-2", refreshed.response.CoinbaseMessage)
	}
	if got := probeCount.Load(); got != 2 {
		t.Fatalf("new candidate probe count = %d, want 2", got)
	}
}

func TestCoinbaseCandidateMismatchIsNotCachedUnderRequestedHeight(t *testing.T) {
	useIsolatedCoinbaseState(t)

	const username = "miner"
	var probeCount atomic.Int32
	fetch := func(gotUsername string) coinbaseFetchResult {
		count := int(probeCount.Add(1))
		return successfulCoinbaseResult(gotUsername, 966500+count, fmt.Sprintf("probe-%d", count))
	}

	requested := 966500
	first, err := getCoinbaseProof(context.Background(), username, candidateHeight(requested), fetch)
	if err != nil {
		t.Fatalf("first proof: %v", err)
	}
	if got := *first.response.BlockHeight; got != 966501 {
		t.Fatalf("first actual height = %d, want 966501", got)
	}

	// The returned proof is cached under its decoded height, so a caller that
	// advances to the actual candidate can reuse it deterministically.
	actual := 966501
	reused, err := getCoinbaseProof(context.Background(), username, candidateHeight(actual), fetch)
	if err != nil {
		t.Fatalf("actual-height proof: %v", err)
	}
	if reused.response.CoinbaseMessage != "probe-1" {
		t.Errorf("actual-height proof = %q, want cached probe-1", reused.response.CoinbaseMessage)
	}
	if got := probeCount.Load(); got != 1 {
		t.Fatalf("actual-height cache unexpectedly probed; count = %d", got)
	}

	// A request still claiming 966500 must not hit a value falsely stored
	// under the stale requested height.
	second, err := getCoinbaseProof(context.Background(), username, candidateHeight(requested), fetch)
	if err != nil {
		t.Fatalf("second proof: %v", err)
	}
	if second.response.CoinbaseMessage != "probe-2" {
		t.Errorf("stale requested height reused %q, want a second probe", second.response.CoinbaseMessage)
	}
	if got := probeCount.Load(); got != 2 {
		t.Fatalf("mismatched candidate probe count = %d, want 2", got)
	}
}

func TestCoinbaseFailedProbeIsNotCached(t *testing.T) {
	useIsolatedCoinbaseState(t)

	var probeCount atomic.Int32
	fetch := func(username string) coinbaseFetchResult {
		count := probeCount.Add(1)
		if count == 1 {
			return coinbaseFetchResult{
				status: http.StatusGatewayTimeout,
				response: CoinbaseResponse{
					Username:  username,
					Timestamp: time.Now().Unix(),
					Error:     "timed out",
				},
			}
		}

		return successfulCoinbaseResult(username, 966500, "recovered")
	}

	height := candidateHeight(966500)
	failed, err := getCoinbaseProof(context.Background(), "miner", height, fetch)
	if err != nil {
		t.Fatalf("failed proof call: %v", err)
	}
	if failed.status != http.StatusGatewayTimeout {
		t.Fatalf("failed status = %d, want %d", failed.status, http.StatusGatewayTimeout)
	}

	recovered, err := getCoinbaseProof(context.Background(), "miner", height, fetch)
	if err != nil {
		t.Fatalf("recovery proof call: %v", err)
	}
	if recovered.response.CoinbaseMessage != "recovered" {
		t.Errorf("recovery response = %q", recovered.response.CoinbaseMessage)
	}
	if got := probeCount.Load(); got != 2 {
		t.Fatalf("probe count = %d, want 2 because failures are not cached", got)
	}
}

func TestHandleCoinbaseRejectsInvalidCandidateHeight(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/coinbase?user=miner&candidate_height=latest", nil)
	rec := httptest.NewRecorder()

	handleCoinbase(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var response CoinbaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "candidate_height must be a positive decimal block height" {
		t.Errorf("error = %q", response.Error)
	}
}

// loadUpdateInterval: the key set, absent (falls back to
// DefaultUpdateInterval), and other non-fatal edge cases (zero, missing
// file, malformed JSON) all degrade the same way loadBtcdRPC does.
func TestLoadUpdateInterval(t *testing.T) {
	dir := t.TempDir()

	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return p
	}

	withKey := write("with-key.conf", `{"update_interval": 15, "btcd": []}`)
	if got := ckconf.LoadUpdateInterval(withKey); got != 15*time.Second {
		t.Errorf("with key set: got %s, want 15s", got)
	}

	withoutKey := write("without-key.conf", `{"btcd": [{"url": "127.0.0.1:8332"}]}`)
	if got := ckconf.LoadUpdateInterval(withoutKey); got != DefaultUpdateInterval {
		t.Errorf("key absent: got %s, want default %s", got, DefaultUpdateInterval)
	}

	zeroKey := write("zero-key.conf", `{"update_interval": 0}`)
	if got := ckconf.LoadUpdateInterval(zeroKey); got != DefaultUpdateInterval {
		t.Errorf("key zero: got %s, want default %s", got, DefaultUpdateInterval)
	}

	if got := ckconf.LoadUpdateInterval(filepath.Join(dir, "does-not-exist.conf")); got != DefaultUpdateInterval {
		t.Errorf("missing conf: got %s, want default %s", got, DefaultUpdateInterval)
	}

	malformed := write("malformed.conf", `{ not json`)
	if got := ckconf.LoadUpdateInterval(malformed); got != DefaultUpdateInterval {
		t.Errorf("malformed conf: got %s, want default %s", got, DefaultUpdateInterval)
	}
}
