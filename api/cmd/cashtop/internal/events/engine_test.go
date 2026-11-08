package events

import (
	"context"
	"strings"
	"testing"
	"time"

	"cashstratumapi/cmd/cashtop/internal/rpc"
	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/poolfmt"
)

type feeder struct {
	e   *Engine
	off int64
}

func (f *feeder) feed(lines []string) []Output {
	var out []Output
	for _, l := range lines {
		f.off += int64(len(l)) + 1
		out = append(out, f.e.Feed(poolfmt.ParseLine(l), f.off)...)
	}
	return out
}

func finds(out []Output) (n int, last BlockFound) {
	for _, o := range out {
		if b, ok := o.(BlockFound); ok {
			n++
			last = b
		}
	}
	return
}

func newGen() *fixturegen.Generator {
	return fixturegen.New(7, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
}

func TestB1OneCelebrationThenReplayNone(t *testing.T) {
	g := newGen()
	finder := fixturegen.FullWorker(1, 2)
	lines := g.BlockCluster(900123, finder, "SV2")
	f := &feeder{e: New(0)}
	out := f.feed(lines)
	n, b := finds(out)
	if n != 1 {
		t.Fatalf("finds=%d", n)
	}
	addr, rig := SplitFinder(finder)
	if b.Height != 900123 || b.Finder != finder || b.Address != addr || b.Rig != rig || b.Proto != "SV2" {
		t.Fatalf("%+v", b)
	}
	if len(b.Hash) != 64 {
		t.Fatalf("hash %q", b.Hash)
	}
	pre := 0
	var luck BlockUpdate
	for _, o := range out {
		switch v := o.(type) {
		case PreSolve:
			pre++
		case BlockUpdate:
			if v.Shares > 0 {
				luck = v
			}
		}
	}
	if pre != 2 {
		t.Fatalf("pre=%d", pre)
	}
	if luck.Height != 900123 {
		t.Fatalf("no luck update: %+v", luck)
	}
	if n2, _ := finds(f.feed(lines)); n2 != 0 {
		t.Fatalf("replay celebrated %d", n2)
	}
}

func TestLuckAndLateHash(t *testing.T) {
	f := &feeder{e: New(0)}
	lines := []string{
		"[2026-01-02 03:04:05.000] Solved and confirmed block 55 by bitcoincash:qaddr.rig.01 on SV1",
		"[2026-01-02 03:04:05.010] Block solved after 1234 shares at 87.5% diff",
		"[2026-01-02 03:04:05.020] Block hash changed to " + strings.Repeat("ab", 32),
	}
	out := f.feed(lines)
	n, b := finds(out)
	if n != 1 || b.Hash != "" || b.Rig != "rig.01" || b.Address != "bitcoincash:qaddr" {
		t.Fatalf("%d %+v", n, b)
	}
	var last BlockUpdate
	for _, o := range out {
		if u, ok := o.(BlockUpdate); ok {
			last = u
		}
	}
	if last.Shares != 1234 || last.LuckPct != 87.5 || last.Hash != strings.Repeat("ab", 32) {
		t.Fatalf("%+v", last)
	}
}

func TestB2RejectedNoCelebration(t *testing.T) {
	f := &feeder{e: New(0)}
	out := f.feed(newGen().RejectedCluster(900124, "high-hash"))
	if n, _ := finds(out); n != 0 {
		t.Fatal("celebrated a reject")
	}
	var rej []BlockRejected
	for _, o := range out {
		if r, ok := o.(BlockRejected); ok {
			rej = append(rej, r)
		}
	}
	if len(rej) != 1 || rej[0].Height != 900124 || rej[0].Reason != "high-hash" {
		t.Fatalf("%+v", rej)
	}
}

func TestB3StartupHistoryNeverCelebrates(t *testing.T) {
	g := newGen()
	hist := append(g.BlockCluster(900001, fixturegen.FullWorker(0, 0), "SV1"), g.BlockCluster(900002, fixturegen.FullWorker(1, 1), "")...)
	var size int64
	for _, l := range hist {
		size += int64(len(l)) + 1
	}
	e := New(size)
	f := &feeder{e: e}
	if n, _ := finds(f.feed(hist)); n != 0 {
		t.Fatalf("history celebrated %d", n)
	}
	if !e.Seen(900001) || !e.Seen(900002) {
		t.Fatal("history not marked seen")
	}
	if n, _ := finds(f.feed(hist)); n != 0 {
		t.Fatal("seen heights celebrated")
	}
	if n, _ := finds(f.feed(g.BlockCluster(900003, fixturegen.FullWorker(0, 0), "SV1"))); n != 1 {
		t.Fatal("new block did not celebrate")
	}
}

func TestSeedSourcesAndScan(t *testing.T) {
	e := New(0, Heights{10}, SolvesIndexSource(strings.NewReader("")), nil)
	e.ScanLines([]string{"[2026-01-02 03:04:05.000] Solved and confirmed block 11 by a.b"})
	f := &feeder{e: e}
	for _, h := range []string{"10", "11"} {
		if n, _ := finds(f.feed([]string{"[2026-01-02 03:04:06.000] Solved and confirmed block " + h + " by a.b"})); n != 0 {
			t.Fatalf("height %s celebrated", h)
		}
	}
}

func TestReplayBypass(t *testing.T) {
	if !Replay(BlockFound{Height: 1}).Replayed {
		t.Fatal("not marked")
	}
}

type fakeNode struct{ hash string }

func (f fakeNode) BlockHash(context.Context, int64) (string, error) { return f.hash, nil }
func (f fakeNode) BlockReward(context.Context, string) (rpc.Reward, error) {
	return rpc.Reward{BCH: 3.125, Satoshis: 312500000}, nil
}
func (f fakeNode) IsOrphaned(_ context.Context, _ int64, h string) (bool, error) {
	return h != f.hash, nil
}

func TestEnrichAndOrphan(t *testing.T) {
	n := fakeNode{hash: "good"}
	b, err := Enrich(context.Background(), n, BlockFound{Height: 5, Hash: "stale"})
	if err != nil || b.Hash != "good" || !b.HasReward || b.Reward != 3.125 {
		t.Fatalf("%+v %v", b, err)
	}
	if o, _ := CheckOrphan(context.Background(), n, b); o {
		t.Fatal("orphan")
	}
	b.Hash = "other"
	if o, _ := CheckOrphan(context.Background(), n, b); !o {
		t.Fatal("not orphan")
	}
}

const niceUser = "bitcoincash:qqnwep7p0802q2e4mpkeze7afer9zj3l8qryyg3chh"

func userLine(stamp string, luck string) string {
	if luck == "" { // the solve-time form
		return "[" + stamp + "] User " + niceUser + `:{"hashrate1m":"4.5T","hashrate5m":"4.4T","hashrate1hr":"4.3T","hashrate1d":"4T","hashrate7d":"4T","shares":175954,"authorised":1700000000}`
	}
	return "[" + stamp + "] User " + niceUser + `:{"hashrate1m":"4.5T","hashrate5m":"4.4T","hashrate1hr":"4.3T","hashrate1d":"4T","hashrate7d":"4T","lastshare":1762480786,"shares":175954,"roundshares":1966086000000,"luck":` + luck + `,"networkdiff":552305174002.9905,"authorised":1700000000}`
}

// A live find carries the finder's own effort from its last periodic User
// line; the solve-time User line without luck does not disturb it, and the
// update that brings the pool effort keeps it.
func TestFinderLuckOnLiveFind(t *testing.T) {
	f := &feeder{e: New(0)}
	out := f.feed([]string{
		userLine("2026-11-07 01:59:46.947", "355.978196935985"),
		userLine("2026-11-07 01:59:50.000", ""),
		"[2026-11-07 02:00:00.646] Solved and confirmed block 976298 by qqnwep7p0802q2e4mpkeze7afer9zj3l8qryyg3chh.nice on SV1",
		userLine("2026-11-07 02:00:00.646", ""),
		"[2026-11-07 02:00:00.646] Block solved after 65606500000 shares at 11.9% diff",
	})
	n, b := finds(out)
	if n != 1 || !b.FinderLuckOK || b.FinderLuck != 355.978196935985 {
		t.Fatalf("%d %+v", n, b)
	}
	var u BlockUpdate
	for _, o := range out {
		if x, ok := o.(BlockUpdate); ok {
			u = x
		}
	}
	if u.LuckPct != 11.9 || !u.FinderLuckOK || u.FinderLuck != 355.978196935985 {
		t.Fatalf("update %+v", u)
	}

	// The finder's round restarted: its next solve before a new User line
	// has no known effort.
	_, b = finds(f.feed([]string{"[2026-11-07 02:00:30.000] Solved and confirmed block 976299 by " + niceUser + ".nice on SV1"}))
	if b.Height != 976299 || b.FinderLuckOK {
		t.Fatalf("second find %+v", b)
	}
}

// Luck seeded from the index serves a find whose User line sits before the
// engine's start, and a solve in the history consumes it.
func TestSeedLuck(t *testing.T) {
	e := New(0)
	e.SeedLuck(map[string]float64{niceUser: 42.5})
	f := &feeder{e: e}
	_, b := finds(f.feed([]string{"[2026-11-07 02:00:00.646] Solved and confirmed block 976298 by qqnwep7p0802q2e4mpkeze7afer9zj3l8qryyg3chh.nice on SV1"}))
	if !b.FinderLuckOK || b.FinderLuck != 42.5 {
		t.Fatalf("%+v", b)
	}

	h := New(1 << 40)
	h.SeedLuck(map[string]float64{"qqnwep7p0802q2e4mpkeze7afer9zj3l8qryyg3chh": 42.5})
	hf := &feeder{e: h}
	hf.feed([]string{"[2026-11-07 02:00:00.646] Solved and confirmed block 976300 by " + niceUser + ".nice on SV1"})
	if _, ok := h.lucks["qqnwep7p0802q2e4mpkeze7afer9zj3l8qryyg3chh"]; ok {
		t.Fatal("a solve in the history must consume the luck")
	}
}
