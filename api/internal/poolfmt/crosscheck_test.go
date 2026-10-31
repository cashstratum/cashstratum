package poolfmt_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/poolfmt"
)

var start = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func newGen() *fixturegen.Generator {
	g := fixturegen.New(42, start)
	g.SetUsers(3, 2)
	return g
}

func parseAll(t *testing.T, lines []string) []poolfmt.Event {
	t.Helper()
	var out []poolfmt.Event
	for _, l := range lines {
		e := poolfmt.ParseLineIn(l, time.UTC)
		if !e.HasTime {
			t.Errorf("no timestamp: %q", l)
		}
		out = append(out, e)
	}
	return out
}

func count(evs []poolfmt.Event, k poolfmt.Kind) (n int) {
	for _, e := range evs {
		if e.Kind == k {
			n++
		}
	}
	return
}

func TestCrossSimulatedLogParses(t *testing.T) {
	g := newGen()
	sim := g.Simulate(40)
	if len(sim.Lines) == 0 {
		t.Fatal("no lines")
	}
	seen := map[poolfmt.Kind]bool{}
	for _, l := range sim.Lines {
		e := poolfmt.ParseLineIn(l, time.UTC)
		if e.Kind == poolfmt.KindUnknown {
			t.Errorf("unparsed: %q", l)
		}
		seen[e.Kind] = true
	}
	for _, k := range []poolfmt.Kind{poolfmt.KindAuthorised, poolfmt.KindDropped, poolfmt.KindUser, poolfmt.KindWorker,
		poolfmt.KindPool, poolfmt.KindStoredWorkbase, poolfmt.KindZMQBlock, poolfmt.KindNetworkDiff,
		poolfmt.KindBlockHashChanged, poolfmt.KindPossibleSolve, poolfmt.KindSolved, poolfmt.KindSolveShares,
		poolfmt.KindBlockRejected, poolfmt.KindRentalDetected, poolfmt.KindPatternDiff, poolfmt.KindPasswordDiff,
		poolfmt.KindSV2Accepted, poolfmt.KindSubmitNoResponse, poolfmt.KindSolvedNoHeight} {
		if !seen[k] {
			t.Errorf("family %v never produced", k)
		}
	}

	var bulk bytes.Buffer
	if _, err := fixturegen.GenerateLog(&bulk, 256<<10, 7); err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(strings.TrimRight(bulk.String(), "\n"), "\n") {
		if poolfmt.ParseLineIn(l, time.UTC).Kind == poolfmt.KindUnknown {
			t.Fatalf("bulk line unparsed: %q", l)
		}
	}
}

func TestCrossTickAndDropped(t *testing.T) {
	g := newGen()
	evs := parseAll(t, g.Tick())
	if count(evs, poolfmt.KindUser) != 3 || count(evs, poolfmt.KindPool) != 3 {
		t.Fatalf("tick kinds: %v", evs)
	}
	var ps poolfmt.PoolStatus
	for _, e := range evs {
		if e.Kind == poolfmt.KindUser && (!e.Stats.LastShare.OK || e.Stats.Hashrate1m <= 0) {
			t.Errorf("user: %+v", e)
		}
		if e.Kind == poolfmt.KindPool {
			ps.HasStatus = ps.HasStatus || e.Pool.HasStatus
			ps.HasHashrate = ps.HasHashrate || e.Pool.HasHashrate
			ps.HasShares = ps.HasShares || e.Pool.HasShares
		}
	}
	if !ps.HasStatus || !ps.HasHashrate || !ps.HasShares {
		t.Error("pool variants")
	}
	d := poolfmt.ParseLineIn(g.Dropped(true, true), time.UTC)
	if d.Kind != poolfmt.KindDropped || !d.Lazily || !d.Reason.OK || !d.Age.OK || d.User == "" {
		t.Errorf("%+v", d)
	}
	d = poolfmt.ParseLineIn(g.Dropped(false, false), time.UTC)
	if d.Kind != poolfmt.KindDropped || d.Lazily || d.Reason.OK {
		t.Errorf("%+v", d)
	}
}

func TestCrossBlockCluster(t *testing.T) {
	g := newGen()
	finder := fixturegen.FullWorker(1, 0)
	evs := parseAll(t, g.BlockCluster(975091, finder, "SV1"))
	for i, e := range evs {
		if e.Kind == poolfmt.KindUnknown {
			t.Errorf("line %d unparsed", i)
		}
	}
	if count(evs, poolfmt.KindSolved) != 1 || count(evs, poolfmt.KindSolveShares) != 1 || count(evs, poolfmt.KindBlockAccepted) != 1 {
		t.Fatalf("cluster counts: %v", evs)
	}
	for _, e := range evs {
		if e.Kind == poolfmt.KindSolved {
			a, _ := poolfmt.SplitWorker(finder)
			if e.Height != 975091 || e.Worker != finder || e.Protocol != "SV1" || e.Address != a {
				t.Errorf("%+v", e)
			}
		}
	}
	// Submitting-share is logged after Solved.
	si, pi := -1, -1
	for i, e := range evs {
		if e.Kind == poolfmt.KindSolved {
			si = i
		}
		if e.Kind == poolfmt.KindSubmitting && e.Extra == "share" {
			pi = i
		}
	}
	if pi < si {
		t.Errorf("share submit at %d before solved at %d", pi, si)
	}
	evs = parseAll(t, newGen().BlockCluster(5, finder, ""))
	for _, e := range evs {
		if e.Kind == poolfmt.KindSolved && e.Protocol != "" {
			t.Errorf("proto %q", e.Protocol)
		}
	}
}

func TestCrossRejectedCluster(t *testing.T) {
	evs := parseAll(t, newGen().RejectedCluster(975092, "high-hash"))
	if count(evs, poolfmt.KindSolved) != 0 || count(evs, poolfmt.KindSubmitReturned) != 1 || count(evs, poolfmt.KindBlockRejected) != 1 {
		t.Fatalf("%v", evs)
	}
	for _, e := range evs {
		switch e.Kind {
		case poolfmt.KindSubmitReturned:
			if e.Message != "high-hash" {
				t.Error(e.Message)
			}
		case poolfmt.KindBlockRejected:
			if e.Height != 975092 {
				t.Error(e.Height)
			}
		case poolfmt.KindUnknown:
			t.Error("unparsed line")
		}
	}
}

func TestCrossUserFile(t *testing.T) {
	g := newGen()
	b := g.UserFile(1)
	u, err := poolfmt.ParseUserFile(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Worker) != 2 || u.Workers.V != 2 || u.Hashrate1m <= 0 || !u.NetworkDiff.OK || u.NetworkDiff.V != g.NetworkDiff {
		t.Errorf("%+v", u)
	}
	for i, w := range u.Worker {
		if w.Name != fixturegen.BareAddr(1)+"."+fixturegen.Rig(i) {
			t.Errorf("worker name %q", w.Name)
		}
		if poolfmt.NormalizeAddress(fixturegen.Addr(1)) != strings.SplitN(w.Name, ".", 2)[0] {
			t.Error("address join")
		}
	}
	if u.Reject.Present || u.Worker[0].Connects.OK {
		t.Error("generator emits no newer-pool keys; presence must be false")
	}
}

func TestCrossPoolStatus(t *testing.T) {
	g := newGen()
	g.Advance(90 * time.Second)
	g.Tick()
	p, err := poolfmt.ParsePoolStatus(bytes.NewReader(g.PoolStatus()))
	if err != nil {
		t.Fatal(err)
	}
	if p.Runtime <= 0 || p.LastUpdate <= 0 || p.Hashrate1m <= 0 || p.Users != 3 {
		t.Errorf("%+v", p)
	}
	pl := g.Pool()
	for _, e := range parseAll(t, pl[:]) {
		if e.Kind != poolfmt.KindPool {
			t.Errorf("pool line parsed as %v", e.Kind)
		}
	}
}

func TestCrossShareLine(t *testing.T) {
	g := newGen()
	ok, err := poolfmt.ParseSharelogLine(g.ShareLine(7700000000000000123, true, 5000, 6000.5))
	if err != nil {
		t.Fatal(err)
	}
	if ok.WorkInfoID != 7700000000000000123 || !ok.Result || ok.Diff != 5000 || ok.SDiff != 6000.5 || ok.Errn != 0 ||
		ok.VersionMask != "1fffe000" || len(ok.Hash) != 64 || ok.IsBlock(7000) || !ok.IsBlock(6000) {
		t.Errorf("%+v", ok)
	}
	bad, err := poolfmt.ParseSharelogLine(g.ShareLine(1, false, 5000, 10))
	if err != nil || bad.Result || bad.Errn != 5 || bad.Error != "Above target" || bad.IsBlock(1) {
		t.Errorf("%+v %v", bad, err)
	}
	blk, _ := poolfmt.ParseSharelogLine(g.ShareLine(1, true, 5000, g.NetworkDiff*1.05))
	if !blk.IsBlock(g.NetworkDiff) {
		t.Error("block share")
	}
	dir, file, _ := strings.Cut(fixturegen.SharelogPath(975091, 7700000000000000123), "/")
	if h, w, ok := poolfmt.ParseSharelogName(dir, file); !ok || h != 975091 || w != 7700000000000000123 {
		t.Error("sharelog path", h, w, ok)
	}
}

func TestCrossHeightsAndSolves(t *testing.T) {
	heights := []int{975099, 975091, 975091, 970702}
	hs, err := poolfmt.ParseSolvedHeights(bytes.NewReader(fixturegen.SolvedHeightsIdx(heights)))
	if err != nil || len(hs) != 3 || hs[0] != 970702 || hs[1] != 975091 || hs[2] != 975099 {
		t.Errorf("%v %v", hs, err)
	}

	hash := fixturegen.Hash(9)
	finder := fixturegen.FullWorker(0, 1)
	file := fixturegen.SolveFile(975091, hash, finder, "2026-03-04 23:30:02", []string{"[x] one\n", "[x] two\n"})
	h, err := poolfmt.ParseSolveHeader(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if h.Height != 975091 || h.Hash != hash || h.ZMQHash != hash || h.Finder != finder || h.Outcome != "ACCEPTED" ||
		h.Confirmations.V != 12 || h.Preserved != "2026-03-04 23:30:02 UTC" || h.Window != "lines 1..2" {
		t.Errorf("%+v", h)
	}
	if ph, phash, ok := poolfmt.ParseSolveFilename(fixturegen.SolveFileName(975091, hash)); !ok || ph != 975091 || phash != hash {
		t.Error("solve filename")
	}

	rows := []fixturegen.SolveIndexRow{
		{Height: 975091, Hash: hash, Timestamp: "2026-03-04 05:06:07.123", Outcome: "ACCEPTED", Finder: finder, Confirmations: 12},
		{Height: 975092, Hash: fixturegen.Hash(10), Timestamp: "2026-03-04 06:06:07.123", Outcome: "ORPHANED", Finder: finder, Confirmations: -1},
	}
	es, err := poolfmt.ParseSolvesIndexIn(bytes.NewReader(fixturegen.SolveIndex(rows)), time.UTC)
	if err != nil || len(es) != 2 {
		t.Fatal(es, err)
	}
	for i, e := range es {
		r := rows[i]
		if e.Height != int64(r.Height) || e.Hash != r.Hash || e.Outcome != r.Outcome || e.Finder != r.Finder ||
			e.Confirmations.V != int64(r.Confirmations) || !e.HasTime || e.ContextFile != fixturegen.SolveFileName(r.Height, r.Hash) {
			t.Errorf("row %d: %+v", i, e)
		}
	}
	if !es[0].Time.Equal(time.Date(2026, 3, 4, 5, 6, 7, 123e6, time.UTC)) {
		t.Error(es[0].Time)
	}
}

func TestCrossSimulatedFindsMatchLog(t *testing.T) {
	g := newGen()
	sim := g.Simulate(30)
	var solved []poolfmt.Event
	for _, l := range sim.Lines {
		if e := poolfmt.ParseLineIn(l, time.UTC); e.Kind == poolfmt.KindSolved {
			solved = append(solved, e)
		}
	}
	if len(solved) != len(sim.Found) || len(solved) == 0 {
		t.Fatalf("solved %d, found %d", len(solved), len(sim.Found))
	}
	for i, f := range sim.Found {
		if solved[i].Height != int64(f.Height) || solved[i].Worker != f.Finder {
			t.Errorf("%d: %+v vs %v", i, solved[i], f)
		}
	}
	rej := 0
	for _, l := range sim.Lines {
		if e := poolfmt.ParseLineIn(l, time.UTC); e.Kind == poolfmt.KindBlockRejected {
			if rej >= len(sim.Rejected) || e.Height != int64(sim.Rejected[rej]) {
				t.Errorf("rejected %+v", e)
			}
			rej++
		}
	}
	if rej != len(sim.Rejected) {
		t.Errorf("rejected %d want %d", rej, len(sim.Rejected))
	}
	for path, lines := range sim.Shares {
		dir, file, _ := strings.Cut(path, "/")
		if _, _, ok := poolfmt.ParseSharelogName(dir, file); !ok {
			t.Errorf("path %q", path)
		}
		for _, l := range lines {
			if _, err := poolfmt.ParseSharelogLine(l); err != nil {
				t.Errorf("share line: %v", err)
			}
		}
	}
}
