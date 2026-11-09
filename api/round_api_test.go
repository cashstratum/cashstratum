package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"cashstratumapi/internal/noderpc"
	"cashstratumapi/internal/poolindex"
	"cashstratumapi/internal/solvecheck"
)

const (
	roundCash   = "qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a" // CashAddr spec vector
	roundLegacy = "1BpEi6DfDAUFd7GtittLSdBeYJvcoaVggu"         // the same address, legacy
	roundOther  = "qqfvksbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// fakeNode is a roundNode with canned answers; err makes every call fail.
type fakeNode struct {
	err      error
	template int64
	diff     float64
	tip      int64
	hashes   map[int64]string
	coinbase map[string]noderpc.BlockCoinbase
}

func (f *fakeNode) GetBlockTemplateValue(context.Context) (int64, error) { return f.template, f.err }
func (f *fakeNode) GetDifficulty(context.Context) (float64, error) {
	if f.diff <= 0 && f.err == nil {
		return 0, errors.New("no difficulty")
	}
	return f.diff, f.err
}
func (f *fakeNode) GetBlockCount(context.Context) (int64, error) { return f.tip, f.err }
func (f *fakeNode) GetBlockHash(_ context.Context, h int64) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if v, ok := f.hashes[h]; ok {
		return v, nil
	}
	return "", errors.New("out of range")
}
func (f *fakeNode) GetBlockCoinbase(_ context.Context, hash string) (noderpc.BlockCoinbase, error) {
	if f.err != nil {
		return noderpc.BlockCoinbase{}, f.err
	}
	if v, ok := f.coinbase[hash]; ok {
		return v, nil
	}
	return noderpc.BlockCoinbase{}, errors.New("unknown block")
}

type roundEnv struct {
	t     *testing.T
	ix    *poolindex.Index
	users *usersEnv
	log   string
	logs  string
	now   time.Time
	key   string
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func (e *roundEnv) share(height int64, user, rig string, diff, sdiff float64, at time.Time) {
	e.t.Helper()
	dir := filepath.Join(e.logs, fmt.Sprintf("%08x", height))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	appendTo(e.t, filepath.Join(dir, "0000000000000001.sharelog"),
		fmt.Sprintf(`{"workinfoid":1,"diff":%v,"sdiff":%v,"result":true,"createdate":"%d,%d","workername":"%s.%s","username":"%s","address":"10.0.0.9"}`+"\n",
			diff, sdiff, at.Unix(), at.Nanosecond(), user, rig, user))
}

func solveLogLine(stamp string, height int64, worker string) string {
	return fmt.Sprintf("[%s] Solved and confirmed block %d by %s on SV1\n", stamp, height, worker)
}

// newRoundEnv opens a real index on a pool with one baseline solve (976456)
// and wires the round routes to it. The clock stands a minute after the
// last share written by the test helpers.
func newRoundEnv(t *testing.T) *roundEnv {
	t.Helper()
	root := t.TempDir()
	e := &roundEnv{t: t, logs: filepath.Join(root, "logs"), key: useTestAPIKey(t)}
	if err := os.MkdirAll(e.logs, 0o755); err != nil {
		t.Fatal(err)
	}
	e.log = filepath.Join(e.logs, "cashstratum.log")
	appendTo(t, e.log, solveLogLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+roundOther+".nice"))
	e.users = newUsersEnv(t)
	ix, err := poolindex.Open(poolindex.Options{Root: root, CacheDir: filepath.Join(t.TempDir(), "c"), RoundStore: filepath.Join(t.TempDir(), "r")})
	if err != nil {
		t.Fatal(err)
	}
	e.ix = ix
	origIx, origRPC, origNow, origLimiter := roundIx, roundRPC, roundNow, rateLimiter
	rateLimiter = NewRateLimiter(100000)
	roundIx, roundRPC = ix, nil
	resetRoundCache()
	t.Cleanup(func() {
		ix.Close()
		roundIx, roundRPC, roundNow, rateLimiter = origIx, origRPC, origNow, origLimiter
		resetRoundCache()
	})
	return e
}

// start is the baseline solve's time, as the index read it.
func (e *roundEnv) start() time.Time {
	e.t.Helper()
	lr := e.ix.LiveRound(time.Now())
	if lr.StartedAt.IsZero() {
		e.t.Fatal("baseline solve not indexed")
	}
	return lr.StartedAt
}

func (e *roundEnv) get(target string) (*httptest.ResponseRecorder, map[string]any) {
	e.t.Helper()
	resetRoundCache()
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, authorizedRequest(http.MethodGet, target, e.key, "192.0.2.1:1234"))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		e.t.Fatalf("%s: body not JSON: %v: %s", target, err, rec.Body.String())
	}
	return rec, body
}

func keysOf(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func wantKeys(t *testing.T, what string, m map[string]any, keys ...string) {
	t.Helper()
	sort.Strings(keys)
	if got := keysOf(m); !reflect.DeepEqual(got, keys) {
		t.Errorf("%s keys\n got %v\nwant %v", what, got, keys)
	}
}

func TestRoundLiveShapeAndMerging(t *testing.T) {
	e := newRoundEnv(t)
	t0 := e.start()
	e.share(976457, "bitcoincash:"+roundCash, "a", 100, 150, t0.Add(time.Second))
	e.share(976457, roundLegacy, "b", 200.5, 900, t0.Add(2*time.Second))
	e.share(976457, roundOther, "x", 0.25, 1, t0.Add(3*time.Second))
	if err := e.ix.Sync(); err != nil {
		t.Fatal(err)
	}
	e.users.writeUser(roundCash, `{"hashrate1m":"3T","hashrate1hr":"2T","lastshare":1,"workers":1,"shares":1}`)
	roundNow = func() time.Time { return t0.Add(time.Minute) }
	roundRPC = &fakeNode{template: 312990000}

	rec, body := e.get("/round")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	wantKeys(t, "round", body, "timestamp", "last_block_height", "started_at", "total_work", "network_difficulty",
		"coinbase_value_sats", "hashrate_1m", "hashrate_1h", "addresses")
	if body["total_work"] != "300.75" {
		t.Errorf("total_work %v", body["total_work"])
	}
	if body["coinbase_value_sats"] != "312990000" {
		t.Errorf("coinbase_value_sats %v", body["coinbase_value_sats"])
	}
	if body["last_block_height"] != float64(976456) || body["started_at"] != float64(t0.Unix()) ||
		body["timestamp"] != float64(t0.Add(time.Minute).Unix()) {
		t.Errorf("heights/times %v %v %v", body["last_block_height"], body["started_at"], body["timestamp"])
	}
	// Pool figures come from the daemon's pool.status (108P / 105P).
	if body["hashrate_1m"] != "108000000000000000" || body["hashrate_1h"] != "105000000000000000" {
		t.Errorf("pool hashrates %v %v", body["hashrate_1m"], body["hashrate_1h"])
	}
	rows := body["addresses"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows %v", rows)
	}
	a := rows[0].(map[string]any)
	wantKeys(t, "row", a, "address", "username", "work", "shares", "last_share_at", "best_share", "best_of_net", "online", "hashrate_1h")
	if a["address"] != roundCash || a["work"] != "300.5" || a["shares"] != "2" || a["online"] != true {
		t.Errorf("merged row %v", a)
	}
	if a["username"] != roundLegacy {
		t.Errorf("username %v, want the newest spelling as submitted", a["username"])
	}
	if a["last_share_at"] != float64(t0.Add(2*time.Second).Unix()) || a["best_share"] != float64(900) {
		t.Errorf("row times %v", a)
	}
	if a["hashrate_1h"] != "2000000000000" {
		t.Errorf("hashrate_1h %v", a["hashrate_1h"])
	}
	b := rows[1].(map[string]any)
	if b["hashrate_1h"] != nil || b["work"] != "0.25" {
		t.Errorf("second row %v", b)
	}
}

func TestRoundNodeDownGivesNulls(t *testing.T) {
	e := newRoundEnv(t)
	roundNow = func() time.Time { return e.start().Add(time.Minute) }
	roundRPC = &fakeNode{err: errors.New("connection refused")}
	rec, body := e.get("/round")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if v, ok := body["coinbase_value_sats"]; !ok || v != nil {
		t.Errorf("coinbase_value_sats %v present=%v, want null", v, ok)
	}
	if rows, ok := body["addresses"].([]any); !ok || len(rows) != 0 {
		t.Errorf("addresses %v, want []", body["addresses"])
	}
}

func TestRoundNetworkDifficultyFromNodeOrNull(t *testing.T) {
	e := newRoundEnv(t)
	roundNow = func() time.Time { return e.start().Add(time.Minute) }
	roundRPC = &fakeNode{template: 1, diff: 5.5e11}
	if _, body := e.get("/round"); body["network_difficulty"] != 5.5e11 {
		t.Errorf("network_difficulty %v, want the node's 5.5e11", body["network_difficulty"])
	}
	roundRPC = &fakeNode{template: 1}
	if _, body := e.get("/round"); body["network_difficulty"] != nil {
		t.Errorf("network_difficulty %v, want null when unknown", body["network_difficulty"])
	}
	roundRPC = nil
	if _, body := e.get("/round"); body["network_difficulty"] != nil {
		t.Errorf("network_difficulty %v, want null without a node", body["network_difficulty"])
	}
}

func TestRoundRoutesNeedAuthAndIndex(t *testing.T) {
	e := newRoundEnv(t)
	for _, p := range []string{"/round", "/rounds/solved", "/round/976460", "/round/history"} {
		rec := httptest.NewRecorder()
		newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without key = %d", p, rec.Code)
		}
	}
	roundIx = nil
	for _, p := range []string{"/round", "/rounds/solved", "/round/976460", "/round/history"} {
		rec, body := e.get(p)
		if rec.Code != http.StatusServiceUnavailable || body["error"] == nil {
			t.Errorf("%s with no index = %d %v", p, rec.Code, body)
		}
	}
}

// solveOnce adds a second solve (976460) with two address spellings.
func (e *roundEnv) solveOnce() time.Time {
	e.t.Helper()
	t0 := e.start()
	t1 := t0.Add(10 * time.Minute)
	e.share(976457, "bitcoincash:"+roundCash, "a", 1000.25, 2000, t0.Add(time.Second))
	e.share(976458, roundLegacy, "b", 0.5, 3, t0.Add(2*time.Minute))
	e.share(976460, roundOther, "finder", 7, 9e12, t1.Add(-time.Millisecond))
	appendTo(e.t, e.log, solveLogLine(t1.Format("2006-01-02 15:04:05.000"), 976460, "bitcoincash:"+roundOther+".finder"))
	if err := e.ix.Sync(); err != nil {
		e.t.Fatal(err)
	}
	return t1
}

func TestRoundsSolvedAndByHeight(t *testing.T) {
	e := newRoundEnv(t)
	t1 := e.solveOnce()
	e.ix.SetBlockHash(976460, "00ab")
	roundNow = func() time.Time { return t1.Add(time.Minute) }

	rec, body := e.get("/rounds/solved?since=976456")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	wantKeys(t, "solved", body, "solves", "timestamp")
	solves := body["solves"].([]any)
	if len(solves) != 1 {
		t.Fatalf("solves %v", solves)
	}
	s := solves[0].(map[string]any)
	wantKeys(t, "solve", s, "height", "hash", "solved_at", "status")
	if s["height"] != float64(976460) || s["hash"] != "00ab" || s["status"] != "accepted" || s["solved_at"] != float64(t1.Unix()) {
		t.Errorf("solve %v", s)
	}
	if _, body := e.get("/rounds/solved?since=976460"); len(body["solves"].([]any)) != 0 {
		t.Errorf("since the newest: %v", body["solves"])
	}
	if rec, _ := e.get("/rounds/solved?since=abc"); rec.Code != 400 {
		t.Errorf("since=abc = %d", rec.Code)
	}
	if rec, _ := e.get("/rounds/solved?since=-1"); rec.Code != 400 {
		t.Errorf("since=-1 = %d", rec.Code)
	}

	rec, body = e.get("/round/976460")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	wantKeys(t, "frozen", body, "height", "hash", "status", "solved_at", "prev_block_height", "started_at",
		"finder_address", "finder_worker", "coinbase_value_sats", "finder_output", "payroll_output", "total_work", "addresses")
	if body["prev_block_height"] != float64(976456) || body["finder_address"] != roundOther {
		t.Errorf("frozen %v", body)
	}
	if body["coinbase_value_sats"] != nil || body["finder_output"] != nil || body["payroll_output"] != nil {
		t.Errorf("node fields before the node answered: %v", body)
	}
	if body["total_work"] != "1007.75" {
		t.Errorf("total_work %v", body["total_work"])
	}
	rows := body["addresses"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows %v", rows)
	}
	r0 := rows[0].(map[string]any)
	wantKeys(t, "frozen row", r0, "address", "username", "work", "shares", "last_share_at")
	if r0["address"] != roundCash || r0["work"] != "1000.75" || r0["shares"] != "2" {
		t.Errorf("merged frozen row %v", r0)
	}

	// The node answers: the outputs and coinbase value are filled once.
	node := &fakeNode{coinbase: map[string]noderpc.BlockCoinbase{"00ab": {Outputs: []noderpc.BlockOutput{
		{Address: "bitcoincash:" + roundOther, Vout: 0, Sats: 156495000},
		{Address: "bitcoincash:" + roundCash, Vout: 1, Sats: 156495000},
		{Vout: 2},
	}}}}
	e.ix.SetBlockHash(976460, "")
	node.tip = 976461
	node.hashes = map[int64]string{976460: "00ab"}
	sweepSolves(context.Background(), e.ix, node, testOwner)
	_, body = e.get("/round/976460")
	if body["coinbase_value_sats"] != "312990000" {
		t.Errorf("coinbase_value_sats %v", body["coinbase_value_sats"])
	}
	fo := body["finder_output"].(map[string]any)
	wantKeys(t, "finder_output", fo, "address", "vout", "value_sats")
	if fo["address"] != roundOther || fo["vout"] != float64(0) || fo["value_sats"] != "156495000" {
		t.Errorf("finder_output %v", fo)
	}
	if po := body["payroll_output"].(map[string]any); po["vout"] != float64(1) || po["address"] != roundCash {
		t.Errorf("payroll_output %v", po)
	}
	sweepSolves(context.Background(), e.ix, &fakeNode{err: errors.New("down")}, testOwner)
	if fr, _ := e.ix.FrozenRound(976460); fr.CoinbaseSats == nil || *fr.CoinbaseSats != 312990000 {
		t.Errorf("fill was not kept: %+v", fr)
	}

	for _, p := range []string{"/round/976457", "/round/1"} {
		if rec, body := e.get(p); rec.Code != 404 || body["error"] == nil {
			t.Errorf("%s = %d %v", p, rec.Code, body)
		}
	}
	if rec, _ := e.get("/round/abc"); rec.Code != 400 {
		t.Errorf("/round/abc = %d", rec.Code)
	}
}

// pointsIndex overrides the two series of a roundIndex.
type pointsIndex struct {
	roundIndex
	hash, best []poolindex.Point
}

func inWindow(ps []poolindex.Point, from, to time.Time) []poolindex.Point {
	var out []poolindex.Point
	for _, p := range ps {
		if p.T >= from.Unix() && (to.IsZero() || p.T < to.Unix()) {
			out = append(out, p)
		}
	}
	return out
}

func (p pointsIndex) HashrateSeries(from, to time.Time) []poolindex.Point {
	return inWindow(p.hash, from, to)
}
func (p pointsIndex) BestShareSeries(from, to time.Time) []poolindex.Point {
	return inWindow(p.best, from, to)
}

func TestRoundHistory(t *testing.T) {
	e := newRoundEnv(t)
	now := time.Unix(1793000000+7*60, 0) // 7 minutes into a 10-minute bucket
	base := now.Unix() / 600 * 600
	roundNow = func() time.Time { return now }
	roundIx = pointsIndex{roundIndex: e.ix,
		hash: []poolindex.Point{{T: base - 600, V: 100}, {T: base - 540, V: 200}, {T: base + 60, V: 50}},
		best: []poolindex.Point{{T: base - 600, V: 7}, {T: base - 60, V: 9.5}, {T: base + 120, V: 3}}}

	rec, body := e.get("/round/history?hours=1&bucket_minutes=10")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	wantKeys(t, "history", body, "bucket_minutes", "buckets", "timestamp")
	if body["bucket_minutes"] != float64(10) {
		t.Errorf("bucket_minutes %v", body["bucket_minutes"])
	}
	bs := body["buckets"].([]any)
	if len(bs) != 7 {
		t.Fatalf("%d buckets, want 7 (six full hours-worth plus the current one)", len(bs))
	}
	for i, b := range bs {
		m := b.(map[string]any)
		wantKeys(t, "bucket", m, "bucket_start", "hashrate", "best_share", "partial")
		start := m["bucket_start"].(float64)
		if int64(start)%600 != 0 || (i > 0 && start != bs[i-1].(map[string]any)["bucket_start"].(float64)+600) {
			t.Errorf("bucket %d start %v not aligned/ordered", i, start)
		}
		if m["partial"] != (i == len(bs)-1) {
			t.Errorf("bucket %d partial %v", i, m["partial"])
		}
	}
	prev := bs[len(bs)-2].(map[string]any)
	if prev["bucket_start"] != float64(base-600) || prev["hashrate"] != "150" || prev["best_share"] != "9.5" {
		t.Errorf("previous bucket %v", prev)
	}
	cur := bs[len(bs)-1].(map[string]any)
	if cur["hashrate"] != "50" || cur["best_share"] != "3" {
		t.Errorf("current bucket %v", cur)
	}
	empty := bs[0].(map[string]any)
	if empty["hashrate"] != nil || empty["best_share"] != nil {
		t.Errorf("empty bucket %v", empty)
	}

	for _, q := range []string{"hours=73", "hours=0", "hours=x", "bucket_minutes=7", "bucket_minutes=0", "hours=1&bucket_minutes=45"} {
		if rec, _ := e.get("/round/history?" + q); rec.Code != 400 {
			t.Errorf("%s = %d, want 400", q, rec.Code)
		}
	}
	if rec, body := e.get("/round/history"); rec.Code != 200 || body["bucket_minutes"] != float64(60) || len(body["buckets"].([]any)) != 25 {
		t.Errorf("defaults: %d %v", rec.Code, body["bucket_minutes"])
	}
}

func TestRoundResponseCached(t *testing.T) {
	e := newRoundEnv(t)
	now := time.Unix(1793000000, 0)
	roundNow = func() time.Time { return now }
	get := func() string {
		rec := httptest.NewRecorder()
		newMux().ServeHTTP(rec, authorizedRequest(http.MethodGet, "/rounds/solved", e.key, "192.0.2.1:1234"))
		return rec.Body.String()
	}
	resetRoundCache()
	first := get()
	now = now.Add(4 * time.Second)
	if get() != first {
		t.Error("response changed inside the cache window")
	}
	now = now.Add(2 * time.Second)
	if get() == first {
		t.Error("response not refreshed after the cache window")
	}
}

// testOwner recognises the pool's coinbases by the payroll address at vout 1.
var testOwner = solvecheck.Owner{PoolAddress: roundCash}

func oursAt(h string) map[string]noderpc.BlockCoinbase {
	return map[string]noderpc.BlockCoinbase{h: {Outputs: []noderpc.BlockOutput{
		{Address: roundOther, Vout: 0, Sats: 100}, {Address: "bitcoincash:" + roundCash, Vout: 1, Sats: 100}}}}
}

func TestSweepByOwnership(t *testing.T) {
	e := newRoundEnv(t)
	e.solveOnce()
	// The record holds the previous block's hash, as the index once wrote it.
	e.ix.SetBlockHash(976460, "00prev")
	solve := func() poolindex.FrozenRound {
		fr, _ := e.ix.FrozenRound(976460)
		return fr
	}
	node := &fakeNode{tip: 976470, hashes: map[int64]string{976460: "00own"}, coinbase: oursAt("00own")}

	node.err = errors.New("down")
	sweepSolves(context.Background(), e.ix, node, testOwner)
	if fr := solve(); fr.Status != "accepted" || fr.Hash != "00prev" || fr.HashVerified {
		t.Fatalf("node down changed the record: %+v", fr)
	}
	node.err = nil
	sweepSolves(context.Background(), e.ix, node, solvecheck.Owner{})
	if fr := solve(); fr.Hash != "00prev" {
		t.Fatalf("undeterminable owner changed the record: %+v", fr)
	}

	sweepSolves(context.Background(), e.ix, node, testOwner)
	fr := solve()
	if fr.Status != "accepted" || fr.Hash != "00own" || !fr.HashVerified || fr.CoinbaseSats == nil || *fr.CoinbaseSats != 200 {
		t.Fatalf("our block: %+v", fr)
	}

	// A real orphan: the chain block at the height pays someone else.
	node.coinbase = map[string]noderpc.BlockCoinbase{"00own": {Outputs: []noderpc.BlockOutput{
		{Address: roundOther, Vout: 0, Sats: 1}, {Address: "bitcoincash:" + roundOther, Vout: 1, Sats: 1}}}}
	sweepSolves(context.Background(), e.ix, node, testOwner)
	if fr := solve(); fr.Status != "orphaned" {
		t.Fatalf("foreign block: %+v", fr)
	}
	e.ix.Sync()
	if lr := e.ix.LiveRound(time.Now()); lr.Height != 976456 {
		t.Errorf("round did not roll forward: height %d", lr.Height)
	}
	// Not yet a block past the height: no verdict.
	node2 := &fakeNode{tip: 976460, hashes: node.hashes, coinbase: node.coinbase}
	e.ix.SetSolveStatus(976460, "accepted")
	sweepSolves(context.Background(), e.ix, node2, testOwner)
	if fr := solve(); fr.Status != "accepted" {
		t.Fatalf("orphaned at the tip: %+v", fr)
	}
	// Ours again after a reorg back.
	node.coinbase = oursAt("00own")
	e.ix.SetSolveStatus(976460, "orphaned")
	sweepSolves(context.Background(), e.ix, node, testOwner)
	if fr := solve(); fr.Status != "accepted" {
		t.Fatalf("re-accept: %+v", fr)
	}
}

func TestRoundOwnerBySignature(t *testing.T) {
	sig := solvecheck.Owner{Sig: "/cashstratum/"}
	cb := noderpc.BlockCoinbase{ScriptSig: "03a1b2c3" + hex.EncodeToString([]byte("/cashstratum/x"))}
	if !sig.Owns(cb) || sig.Owns(noderpc.BlockCoinbase{ScriptSig: "0011"}) || (solvecheck.Owner{}).Known() {
		t.Error("signature ownership wrong")
	}
	if !testOwner.Owns(oursAt("h")["h"]) || testOwner.Owns(noderpc.BlockCoinbase{}) {
		t.Error("address ownership wrong")
	}
}
