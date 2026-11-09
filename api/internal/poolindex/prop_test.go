package poolindex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// One address in its CashAddr and legacy spellings (CashAddr spec vector).
const (
	propCash   = "qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a"
	propLegacy = "1BpEi6DfDAUFd7GtittLSdBeYJvcoaVggu"
)

type propEnv struct {
	t     *testing.T
	root  string
	log   string
	store string
	cache string
}

func newPropEnv(t *testing.T, log string) *propEnv {
	t.Helper()
	root := t.TempDir()
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	p := &propEnv{t: t, root: root, log: filepath.Join(logs, "cashstratum.log"),
		store: filepath.Join(t.TempDir(), "rounds"), cache: filepath.Join(t.TempDir(), "cache")}
	if err := os.WriteFile(p.log, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *propEnv) open(mut ...func(*Options)) *Index {
	p.t.Helper()
	o := Options{Root: p.root, CacheDir: p.cache, RoundStore: p.store}
	for _, m := range mut {
		m(&o)
	}
	ix, err := Open(o)
	if err != nil {
		p.t.Fatal(err)
	}
	return ix
}

// share appends one sharelog line to logs/<height>/0000000000000001.sharelog.
func (p *propEnv) share(height int64, user, rig string, diff, sdiff float64, at time.Time, ok bool) {
	p.t.Helper()
	dir := filepath.Join(p.root, "logs", fmt.Sprintf("%08x", height))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		p.t.Fatal(err)
	}
	appendFile(p.t, filepath.Join(dir, "0000000000000001.sharelog"),
		fmt.Sprintf(`{"workinfoid":1,"diff":%v,"sdiff":%v,"result":%t,"createdate":"%d,%d","workername":"%s.%s","username":"%s","address":"10.0.0.9"}`+"\n",
			diff, sdiff, ok, at.Unix(), at.Nanosecond(), user, rig, user))
}

func solveLine(stamp string, height int64, worker string) string {
	return fmt.Sprintf("[%s] Solved and confirmed block %d by %s on SV1\n", stamp, height, worker)
}

func (p *propEnv) recordBytes(h int64) []byte {
	p.t.Helper()
	raw, err := os.ReadFile(filepath.Join(p.store, fmt.Sprint(h), roundFile))
	if err != nil {
		p.t.Fatal(err)
	}
	return raw
}

func frozenWork(fr FrozenRound) map[string]float64 {
	m := map[string]float64{}
	for _, a := range fr.Addresses {
		m[a.Address] = a.Work
	}
	return m
}

// The live round keys every figure by canonical address: legacy, prefixed
// and upper-case spellings of one address merge into one row, rejected
// shares count nowhere, and online follows the newest accepted share.
func TestLiveRoundCanonicalAddressFields(t *testing.T) {
	solve := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	p := newPropEnv(t, solve)
	t0 := stampOf(t, solve)
	p.share(976457, "bitcoincash:"+propCash, "a", 100, 150, t0.Add(1*time.Second), true)
	p.share(976457, propLegacy, "b", 200, 900, t0.Add(2*time.Second), true)
	p.share(976457, "BITCOINCASH:"+strings.ToUpper(propCash), "c", 300, 250, t0.Add(3*time.Second), true)
	p.share(976457, propLegacy, "b", 5000, 1e9, t0.Add(4*time.Second), false) // rejected
	p.share(976457, addrBare, "x", 0.5, 1, t0.Add(5*time.Second), true)
	ix := p.open()
	defer ix.Close()

	last := t0.Add(3 * time.Second)
	lr := ix.LiveRound(last.Add(time.Minute))
	if lr.Height != 976456 || !lr.StartedAt.Equal(t0) || lr.TotalWork != 600.5 {
		t.Fatalf("round %+v", lr)
	}
	if len(lr.Addresses) != 2 {
		t.Fatalf("rows %+v", lr.Addresses)
	}
	a := lr.Addresses[0]
	want := RoundAddress{Address: propCash, Username: "BITCOINCASH:" + strings.ToUpper(propCash), Work: 600, Shares: 3,
		LastShareAt: last, BestShare: 900, Online: true}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("row\n got %+v\nwant %+v", a, want)
	}
	if b := lr.Addresses[1]; b.Address != addrBare || b.Work != 0.5 || b.Shares != 1 {
		t.Fatalf("second row %+v", b)
	}
	if ix.LiveRound(last.Add(OnlineWindow)).Addresses[0].Online != true {
		t.Fatal("offline exactly at the window edge")
	}
	if ix.LiveRound(last.Add(OnlineWindow + time.Second)).Addresses[0].Online {
		t.Fatal("online past the window")
	}
	if got := ix.Snapshot().RoundSums; !reflect.DeepEqual(got, map[string]float64{propCash: 600, addrBare: 0.5}) {
		t.Fatalf("snapshot sums %v", got)
	}
	if got := ix.Snapshot().RoundAddrBest[propCash]; got != 900 {
		t.Fatalf("snapshot best %v", got)
	}

	// The per-address figures survive a restart from the cache.
	ix.Close()
	ix2 := p.open()
	defer ix2.Close()
	if ix2.Info().Rescanned {
		t.Fatalf("unexpected rescan: %+v", ix2.Info())
	}
	if got := ix2.LiveRound(last).Addresses[0]; got.Shares != 3 || got.Username != want.Username || !got.LastShareAt.Equal(last) {
		t.Fatalf("after restart %+v", got)
	}
}

// A store opened on a pool with history freezes nothing from before it
// existed, then freezes each later solve with the shares created in
// (previous block, solve], the solving share included.
func TestFreezeRoundAtSolve(t *testing.T) {
	soA := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	p := newPropEnv(t, soA)
	t0 := stampOf(t, soA)
	ix := p.open()
	defer ix.Close()
	if s := ix.Solves(0); len(s) != 0 {
		t.Fatalf("history frozen: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(p.store, "976456")); !os.IsNotExist(err) {
		t.Fatalf("baseline solve has a record: %v", err)
	}

	soB := solveLine("2026-11-08 08:10:00.000", 976460, "bitcoincash:"+propCash+".finder")
	t1 := stampOf(t, soB)
	p.share(976456, addrBare, "old", 999, 1, t0.Add(-time.Second), true) // previous round's directory
	p.share(976457, "bitcoincash:"+propCash, "a", 1000.25, 2000, t0.Add(time.Second), true)
	p.share(976458, propLegacy, "b", 0.1, 3, t0.Add(2*time.Minute), true)
	p.share(976459, addrBare, "x", 40, 50, t0.Add(3*time.Minute), true)
	p.share(976460, "bitcoincash:"+propCash, "finder", 7, 9e12, t1.Add(-time.Millisecond), true) // the solving share
	p.share(976461, addrBare, "x", 11, 12, t1.Add(time.Second), true)                            // next round
	appendFile(t, p.log, soB)
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}

	fr, ok := ix.FrozenRound(976460)
	if !ok {
		t.Fatal("solve not frozen")
	}
	if fr.Height != 976460 || fr.Status != StatusAccepted || !fr.SolvedAt.Equal(t1) || fr.PrevBlockHeight != 976456 ||
		!fr.StartedAt.Equal(t0) || fr.FinderAddress != propCash || fr.FinderWorker != "bitcoincash:"+propCash+".finder" {
		t.Fatalf("record header %+v", fr)
	}
	wantWork := map[string]float64{propCash: 1000.25 + 0.1 + 7, addrBare: 40}
	if got := frozenWork(fr); !reflect.DeepEqual(got, wantWork) {
		t.Fatalf("work %v, want %v", got, wantWork)
	}
	if fr.TotalWork != 1000.25+0.1+7+40 {
		t.Fatalf("total %v", fr.TotalWork)
	}
	if a := fr.Addresses[0]; a.Shares != 3 || !a.LastShareAt.Equal(t1.Add(-time.Millisecond)) || a.Username != "bitcoincash:"+propCash {
		t.Fatalf("finder row %+v", a)
	}
	if lr := ix.LiveRound(t1); lr.Height != 976460 || lr.TotalWork != 11 {
		t.Fatalf("live round after the solve %+v", lr)
	}

	// The record on disk carries full float precision and the contract's
	// field names; the round's sharelogs sit beside it.
	var disk map[string]any
	if err := json.Unmarshal(p.recordBytes(976460), &disk); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"height", "hash", "status", "solved_at", "finder_worker", "finder_address", "prev_block_height", "started_at", "total_work", "addresses"} {
		if _, ok := disk[k]; !ok {
			t.Fatalf("record lacks %q: %v", k, disk)
		}
	}
	for _, d := range []string{"000ee649", "000ee64a", "000ee64b", "000ee64c"} {
		if _, err := os.Stat(filepath.Join(p.store, "976460", archiveDir, d, "0000000000000001.sharelog")); err != nil {
			t.Fatalf("archive lacks %s: %v", d, err)
		}
	}
	for _, d := range []string{"000ee648", "000ee64d"} {
		if _, err := os.Stat(filepath.Join(p.store, "976460", archiveDir, d)); !os.IsNotExist(err) {
			t.Fatalf("archive holds another round's directory %s", d)
		}
	}
	if s := ix.Solves(0); len(s) != 1 || s[0].Height != 976460 || s[0].Status != StatusAccepted || !s[0].SolvedAt.Equal(t1) {
		t.Fatalf("solves %+v", s)
	}
	if s := ix.Solves(976460); len(s) != 0 {
		t.Fatalf("since is not exclusive: %+v", s)
	}
}

// Two solves seen in one sync both get a record, each with its own window.
func TestTwoSolvesInOneSyncBothFrozen(t *testing.T) {
	soA := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	p := newPropEnv(t, soA)
	t0 := stampOf(t, soA)
	ix := p.open()
	defer ix.Close()
	soB := solveLine("2026-11-08 08:05:00.000", 976458, "bitcoincash:"+addrBare+".x")
	soC := solveLine("2026-11-08 08:05:02.000", 976459, "bitcoincash:"+propCash+".a")
	t1, t2 := stampOf(t, soB), stampOf(t, soC)
	p.share(976457, addrBare, "x", 10, 1, t0.Add(time.Second), true)
	p.share(976458, addrBare, "x", 20, 1, t1.Add(-time.Second), true)
	p.share(976459, propCash, "a", 30, 1, t1.Add(time.Second), true)
	p.share(976459, propCash, "a", 40, 1, t2.Add(-time.Millisecond), true)
	p.share(976460, propCash, "a", 50, 1, t2.Add(time.Second), true)
	appendFile(t, p.log, soB+soC)
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	b, okB := ix.FrozenRound(976458)
	c, okC := ix.FrozenRound(976459)
	if !okB || !okC {
		t.Fatalf("frozen %v %v", okB, okC)
	}
	if b.PrevBlockHeight != 976456 || !reflect.DeepEqual(frozenWork(b), map[string]float64{addrBare: 30}) {
		t.Fatalf("first record %+v", b)
	}
	if c.PrevBlockHeight != 976458 || !c.StartedAt.Equal(t1) || !reflect.DeepEqual(frozenWork(c), map[string]float64{propCash: 70}) {
		t.Fatalf("second record %+v", c)
	}
	if s := ix.Solves(0); len(s) != 2 || s[0].Height != 976458 || s[1].Height != 976459 {
		t.Fatalf("solves %+v", s)
	}
	if lr := ix.LiveRound(t2); lr.Height != 976459 || lr.TotalWork != 50 {
		t.Fatalf("live %+v", lr)
	}
}

// A rescan rebuilds the live round but never re-freezes or rewrites a
// record, even when the sharelogs now say something else.
func TestFrozenRoundImmutableAcrossRebuild(t *testing.T) {
	soA := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	soB := solveLine("2026-11-08 08:10:00.000", 976460, "bitcoincash:"+propCash+".a")
	p := newPropEnv(t, soA)
	t0 := stampOf(t, soA)
	ix := p.open()
	p.share(976457, propCash, "a", 100, 1, t0.Add(time.Second), true)
	appendFile(t, p.log, soB)
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	before := p.recordBytes(976460)
	ix.Close()

	// The window's sharelog grows after the freeze; a rebuild would differ.
	p.share(976457, propCash, "a", 5000, 1, t0.Add(2*time.Second), true)
	ix2 := p.open(func(o *Options) { o.ForceRescan = true })
	defer ix2.Close()
	if !ix2.Info().Rescanned {
		t.Fatal("no rescan")
	}
	if err := ix2.Sync(); err != nil {
		t.Fatal(err)
	}
	if after := p.recordBytes(976460); !bytes.Equal(before, after) {
		t.Fatalf("record rewritten:\n%s\n%s", before, after)
	}
	if fr, _ := ix2.FrozenRound(976460); fr.TotalWork != 100 {
		t.Fatalf("loaded record %+v", fr)
	}
	if _, err := os.Stat(filepath.Join(p.store, "976456", roundFile)); !os.IsNotExist(err) {
		t.Fatal("rescan froze the baseline solve")
	}

	// Freezing the same height again keeps the existing record.
	ix2.mu.Lock()
	err := ix2.freeze(ix2.st.Blocks[976460])
	ix2.mu.Unlock()
	if err != nil || !bytes.Equal(before, p.recordBytes(976460)) {
		t.Fatalf("re-freeze changed the record: %v", err)
	}
	if err := writeNew(filepath.Join(p.store, "976460", roundFile), []byte("x")); !os.IsExist(err) {
		t.Fatalf("writeNew overwrote: %v", err)
	}

	// A wiped cache with the store intact behaves the same.
	if err := os.RemoveAll(p.cache); err != nil {
		t.Fatal(err)
	}
	ix2.Close()
	ix3 := p.open()
	defer ix3.Close()
	if !bytes.Equal(before, p.recordBytes(976460)) || len(ix3.Solves(0)) != 1 {
		t.Fatal("record changed after the cache was wiped")
	}
}

// Marking the newest solve orphaned updates the block and the frozen record,
// and the live round rolls forward from the previous accepted block,
// carrying the orphaned round's work. Accepting it again undoes that.
func TestOrphanRollsRoundForward(t *testing.T) {
	soA := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	soB := solveLine("2026-11-08 08:10:00.000", 976460, "bitcoincash:"+propCash+".a")
	p := newPropEnv(t, soA)
	t0, t1 := stampOf(t, soA), stampOf(t, soB)
	ix := p.open()
	defer ix.Close()
	p.share(976457, propCash, "a", 100, 1, t0.Add(time.Second), true)
	p.share(976461, addrBare, "x", 7, 1, t1.Add(time.Second), true)
	appendFile(t, p.log, soB)
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	if lr := ix.LiveRound(t1); lr.Height != 976460 || lr.TotalWork != 7 {
		t.Fatalf("before orphan %+v", lr)
	}
	if err := ix.SetSolveStatus(976460, StatusOrphaned); err != nil {
		t.Fatal(err)
	}
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	lr := ix.LiveRound(t1)
	if lr.Height != 976456 || !lr.StartedAt.Equal(t0) || lr.TotalWork != 107 {
		t.Fatalf("round did not roll forward: %+v", lr)
	}
	if b := blockAt(ix.Snapshot(), 976460); b == nil || b.Status != StatusOrphaned {
		t.Fatalf("block %+v", b)
	}
	fr, _ := ix.FrozenRound(976460)
	if fr.Status != StatusOrphaned || fr.TotalWork != 100 {
		t.Fatalf("record %+v", fr)
	}
	if !strings.Contains(string(p.recordBytes(976460)), `"status": "orphaned"`) {
		t.Fatal("record on disk not orphaned")
	}
	if s := ix.Solves(0); len(s) != 1 || s[0].Status != StatusOrphaned {
		t.Fatalf("solves %+v", s)
	}

	// The next solve's round starts after the last accepted block, before
	// the orphan, and holds the orphaned round's work.
	soC := solveLine("2026-11-08 08:20:00.000", 976463, "bitcoincash:"+addrBare+".x")
	t2 := stampOf(t, soC)
	p.share(976463, addrBare, "x", 3, 1, t2.Add(-time.Second), true)
	appendFile(t, p.log, soC)
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	c, ok := ix.FrozenRound(976463)
	if !ok || c.PrevBlockHeight != 976456 || !reflect.DeepEqual(frozenWork(c), map[string]float64{propCash: 100, addrBare: 10}) {
		t.Fatalf("rolled-forward record %+v", c)
	}

	// The verdict survives a restart, and re-accepting restores it.
	ix.Close()
	ix2 := p.open(func(o *Options) { o.ForceRescan = true })
	defer ix2.Close()
	if b := blockAt(ix2.Snapshot(), 976460); b == nil || b.Status != StatusOrphaned {
		t.Fatalf("orphan lost on rescan: %+v", b)
	}
	if err := ix2.SetSolveStatus(976460, StatusAccepted); err != nil {
		t.Fatal(err)
	}
	if fr, _ := ix2.FrozenRound(976460); fr.Status != StatusAccepted || fr.TotalWork != 100 {
		t.Fatalf("re-accepted record %+v", fr)
	}
	if err := ix2.SetSolveStatus(1, StatusOrphaned); err != ErrUnknownSolve {
		t.Fatalf("unknown height: %v", err)
	}
	if err := ix2.SetSolveStatus(976460, StatusRejected); err == nil {
		t.Fatal("rejected accepted as a solve status")
	}
}

// SetBlockHash fills a record frozen without a hash, and never changes one.
func TestFrozenHashFilledOnce(t *testing.T) {
	soA := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	soB := solveLine("2026-11-08 08:10:00.000", 976460, "bitcoincash:"+propCash+".a")
	p := newPropEnv(t, soA)
	ix := p.open()
	defer ix.Close()
	appendFile(t, p.log, soB)
	ix.Sync()
	if fr, _ := ix.FrozenRound(976460); fr.Hash != "" {
		t.Fatalf("hash %q", fr.Hash)
	}
	ix.SetBlockHash(976460, "00ab")
	ix.SetBlockHash(976460, "00cd")
	if fr, _ := ix.FrozenRound(976460); fr.Hash != "00ab" {
		t.Fatalf("hash %q", fr.Hash)
	}
	if !strings.Contains(string(p.recordBytes(976460)), `"hash": "00ab"`) {
		t.Fatal("hash not saved")
	}
}

// The per-minute pool best share keeps the highest accepted sdiff per minute,
// persists, and survives a rescan; the hashrate series is filtered by range.
func TestBestShareSeries(t *testing.T) {
	soA := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	p := newPropEnv(t, soA)
	t0 := stampOf(t, soA)
	m0 := t0.Truncate(time.Minute).Add(time.Minute)
	p.share(976457, propCash, "a", 1, 500, m0.Add(5*time.Second), true)
	p.share(976457, addrBare, "x", 1, 900, m0.Add(50*time.Second), true)
	p.share(976457, addrBare, "x", 1, 1e15, m0.Add(51*time.Second), false) // rejected
	p.share(976457, propCash, "a", 1, 300, m0.Add(70*time.Second), true)
	p.share(976457, propCash, "a", 1, 9e12, m0.Add(-2*time.Hour), true) // before the round
	ix := p.open()
	want := []Point{{T: m0.Unix(), V: 900}, {T: m0.Unix() + 60, V: 300}}
	if got := ix.BestShareSeries(m0, time.Time{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("series %v, want %v", got, want)
	}
	if got := ix.BestShareSeries(m0, m0.Add(time.Minute)); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("bounded series %v", got)
	}
	ix.Close()
	ix2 := p.open(func(o *Options) { o.ForceRescan = true })
	defer ix2.Close()
	if got := ix2.BestShareSeries(time.Time{}, time.Time{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("after rescan %v", got)
	}

	bs := map[int64]float64{0: 1, 60: 2, int64(bestSeriesKeep/time.Second) + 60: 3}
	pruneBestSeries(bs)
	if _, ok := bs[0]; ok || len(bs) != 2 {
		t.Fatalf("prune %v", bs)
	}

	e := newEnv(t)
	ex := e.open()
	defer ex.Close()
	all := ex.Snapshot().Hashrate
	if len(all) < 3 {
		t.Fatalf("fixture has %d hashrate points", len(all))
	}
	from, to := time.Unix(all[1].T, 0), time.Unix(all[len(all)-1].T, 0)
	if got := ex.HashrateSeries(from, to); !reflect.DeepEqual(got, all[1:len(all)-1]) {
		t.Fatalf("hashrate range %d points, want %d", len(got), len(all)-2)
	}
}

func TestMinersPot(t *testing.T) {
	for cb, want := range map[int64][2]int64{ // {payroll, pot}
		0:         {0, 0},
		-5:        {0, 0},
		1091:      {0, 0},                 // payroll 545 is dust: no output
		1092:      {546, 524},             // smallest emitted payroll output
		312990000: {156495000, 150235200}, // even: an exact half
		312990001: {156495000, 150235200}, // odd: the finder takes the extra satoshi
		312585443: {156292721, 150041012},
		625000000: {312500000, 300000000},
	} {
		payroll, pot := PayrollOutput(cb), MinersPot(cb)
		if payroll != want[0] || pot != want[1] {
			t.Fatalf("coinbase %d: payroll %d pot %d, want %v", cb, payroll, pot, want)
		}
		if pot > payroll || (cb > 0 && payroll > cb-payroll) {
			t.Fatalf("coinbase %d: pot %d exceeds payroll %d, or payroll above the finder output", cb, pot, payroll)
		}
	}
}

func TestEstimatePayouts(t *testing.T) {
	sum := func(es []PayoutEstimate) (n int64) {
		for _, e := range es {
			n += e.Sats
		}
		return
	}
	byAddr := func(es []PayoutEstimate) map[string]PayoutEstimate {
		m := map[string]PayoutEstimate{}
		for _, e := range es {
			m[e.Address] = e
		}
		return m
	}

	// A single miner takes the whole pot.
	pot, es := EstimatePayouts(312990000, map[string]float64{"a": 12.5})
	if pot != 150235200 || len(es) != 1 || !es[0].Eligible || es[0].Sats != pot {
		t.Fatalf("single %d %+v", pot, es)
	}

	// Sub-1% work is redistributed to the eligible addresses pro rata.
	pot, es = EstimatePayouts(100_000_000, map[string]float64{"a": 600, "b": 395, "c": 4.99, "d": 0.01, "z": 0})
	m := byAddr(es)
	if !m["a"].Eligible || !m["b"].Eligible || m["c"].Eligible || m["d"].Eligible || m["z"].Eligible {
		t.Fatalf("eligibility %+v", es)
	}
	if pot != 48_000_000 || m["a"].Sats != 48_000_000*600/995 || m["b"].Sats != 48_000_000*395/995 || m["c"].Sats != 0 {
		t.Fatalf("split %d %+v", pot, es)
	}
	if es[0].Address != "a" || es[len(es)-1].Address != "z" {
		t.Fatalf("order %+v", es)
	}

	// Exactly 1% is eligible.
	_, es = EstimatePayouts(100_000_000, map[string]float64{"a": 99, "b": 1})
	if m := byAddr(es); !m["b"].Eligible || m["b"].Sats != 480_000 {
		t.Fatalf("boundary %+v", es)
	}

	// Nobody reaches 1%: everyone with work shares pro rata.
	work := map[string]float64{}
	for i := 0; i < 150; i++ {
		work[fmt.Sprintf("m%03d", i)] = 1
	}
	pot, es = EstimatePayouts(100_000_000, work)
	for _, e := range es {
		if !e.Eligible || e.Sats != pot/150 {
			t.Fatalf("fallback %+v", e)
		}
	}

	// Payouts never exceed the pot, whatever the rounding.
	pot, es = EstimatePayouts(312990001, map[string]float64{"a": 1.0 / 3, "b": 1.0 / 3, "c": 1.0 / 3, "d": 0.1, "e": 0.7})
	if s := sum(es); s > pot || pot-s > int64(len(es)) {
		t.Fatalf("sum %d pot %d", s, pot)
	}

	// No work or no coinbase pays nothing.
	if pot, es := EstimatePayouts(100_000, map[string]float64{"a": 0}); pot != 48_000 || es[0].Sats != 0 || es[0].Eligible {
		t.Fatalf("no work %d %+v", pot, es)
	}
	if pot, es := EstimatePayouts(0, map[string]float64{"a": 5}); pot != 0 || es[0].Sats != 0 || !es[0].Eligible {
		t.Fatalf("no coinbase %d %+v", pot, es)
	}
}

// The node-sourced fields are filled once and never overwritten, and they
// survive in the saved record.
func TestFrozenNodeFilledOnce(t *testing.T) {
	soA := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	soB := solveLine("2026-11-08 08:10:00.000", 976460, "bitcoincash:"+propCash+".a")
	p := newPropEnv(t, soA)
	ix := p.open()
	defer ix.Close()
	appendFile(t, p.log, soB)
	ix.Sync()
	fin := BlockOutput{Address: propCash, Vout: 0, Sats: 100}
	pay := BlockOutput{Address: "qpayroll", Vout: 1, Sats: 100}
	if ok, err := ix.SetFrozenNode(976460, 200, fin, pay); !ok || err != nil {
		t.Fatalf("first fill: %v %v", ok, err)
	}
	if ok, err := ix.SetFrozenNode(976460, 999, pay, fin); ok || err != nil {
		t.Fatalf("second fill: %v %v", ok, err)
	}
	fr, _ := ix.FrozenRound(976460)
	if fr.CoinbaseSats == nil || *fr.CoinbaseSats != 200 || fr.FinderOutput.Sats != 100 || fr.PayrollOutput.Vout != 1 {
		t.Fatalf("record %+v", fr)
	}
	if !strings.Contains(string(p.recordBytes(976460)), `"coinbase_value_sats": 200`) {
		t.Fatal("node fields not saved")
	}
	if _, err := ix.SetFrozenNode(1, 1, fin, pay); err != ErrUnknownSolve {
		t.Fatalf("unknown height: %v", err)
	}
}

// The production ordering: the previous block's hash is logged before the
// solve line and the solve's own hash right after it. The solve must not
// take the earlier one, and the node's verdict corrects it once.
func TestSolveHashIsTheOneLoggedAfter(t *testing.T) {
	const prev = "0000000000000000056fea17c000000000000000000000000000000000000001"
	const own = "0000000000000000020a1b9b01882a2a1be2ca6de2c650fb2d015de349235aaa"
	soA := solveLine("2026-11-08 08:00:00.000", 976456, "bitcoincash:"+addrNice+".nice")
	p := newPropEnv(t, soA)
	ix := p.open()
	defer ix.Close()
	appendFile(t, p.log, "[2026-11-08 08:10:00.100] ZMQ block hash "+prev+", transactions: 3\n")
	appendFile(t, p.log, solveLine("2026-11-08 08:10:35.531", 976460, "bitcoincash:"+propCash+".a"))
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	if fr, _ := ix.FrozenRound(976460); fr.Hash == prev {
		t.Fatal("solve took the previous block's hash")
	}
	appendFile(t, p.log, "[2026-11-08 08:10:35.531] ZMQ block hash "+own+", transactions: 5\n")
	appendFile(t, p.log, "[2026-11-08 08:10:35.532] Block hash changed to "+own+"\n")
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	if fr, _ := ix.FrozenRound(976460); fr.Hash != own || fr.HashVerified {
		t.Fatalf("after own hash line: %+v", fr)
	}

	// A hash line long after the solve is not claimed by it.
	appendFile(t, p.log, solveLine("2026-11-08 08:20:00.000", 976470, "bitcoincash:"+propCash+".a"))
	appendFile(t, p.log, "[2026-11-08 08:20:30.000] ZMQ block hash "+prev+", transactions: 1\n")
	ix.Sync()
	if fr, _ := ix.FrozenRound(976470); fr.Hash != "" {
		t.Fatalf("late hash claimed: %q", fr.Hash)
	}

	// Verification corrects once, then freezes the hash.
	ix.SetBlockHash(976470, "")
	if ok, err := ix.VerifySolveHash(976470, "00aa"); !ok || err != nil {
		t.Fatalf("verify: %v %v", ok, err)
	}
	if ok, _ := ix.VerifySolveHash(976470, "00bb"); ok {
		t.Fatal("verified hash changed twice")
	}
	if fr, _ := ix.FrozenRound(976470); fr.Hash != "00aa" || !fr.HashVerified {
		t.Fatalf("verified record %+v", fr)
	}
	if ok, _ := ix.VerifySolveHash(976460, prev); !ok {
		t.Fatal("provisional hash not correctable")
	}
	if fr, _ := ix.FrozenRound(976460); fr.Hash != prev {
		t.Fatalf("correction %+v", fr)
	}
}
