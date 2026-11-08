package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"cashstratumapi/internal/fixturegen"
)

func TestClassifyDroppedIsOffAtOnce(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	w := WorkerRow{Hashrate1m: 1e12, Hashrate5m: 1e12, LastShare: now.Add(-10 * time.Second), Connected: now.Add(-time.Hour)}
	if Classify(w, now) != StateLive {
		t.Fatal("precondition: live")
	}
	w.Disconnected = now.Add(-5 * time.Second)
	if got := Classify(w, now); got != StateOff {
		t.Fatalf("dropped worker = %v, want off", got)
	}
	w.Connected = now.Add(-2 * time.Second) // reconnected after the drop
	if got := Classify(w, now); got != StateLive {
		t.Fatalf("reconnected worker = %v, want live", got)
	}
	w.Connected = now.Add(-time.Hour)
	w.LastShare = now // a share after the drop wins
	if got := Classify(w, now); got != StateLive {
		t.Fatalf("share after drop = %v, want live", got)
	}
}

func workersFixture(now time.Time) *Snapshot {
	a := fixturegen.Addr(0)
	return &Snapshot{
		Pool: PoolCard{NetworkDiff: 1e12},
		Workers: []WorkerRow{
			{Name: a + ".live", Address: a, Hashrate1m: 2e12, Hashrate5m: 2e12, Diff: 1024, LastShare: now.Add(-3 * time.Second), Connected: now.Add(-time.Hour)},
			{Name: a + ".gone", Address: a, Hashrate1m: 1e12, Hashrate5m: 1e12, Diff: 1024, LastShare: now.Add(-4 * time.Minute), Connected: now.Add(-time.Hour),
				Disconnected: now.Add(-3 * time.Minute), DropReason: "peer_eof"},
			{Name: a + ".old", Address: a, Connected: time.Time{}},
		},
	}
}

func renderW(t *testing.T, snap *Snapshot, now time.Time, show bool) string {
	t.Helper()
	ctx := addrCtx(nil, snap)
	ctx.Now = now
	var out []string
	for _, l := range renderWorkersShow(ctx, 140, 20, 0, show) {
		out = append(out, ansi.Strip(l))
	}
	return strings.Join(out, "\n")
}

func TestWorkersHideOffByDefault(t *testing.T) {
	now := addrNow
	snap := workersFixture(now)
	hidden := renderW(t, snap, now, false)
	if strings.Contains(hidden, "gone") || strings.Contains(hidden, "old") {
		t.Fatalf("off workers listed:\n%s", hidden)
	}
	if !strings.Contains(hidden, "2 off (hidden)") || !strings.Contains(hidden, "live") {
		t.Fatalf("header must count hidden off workers:\n%s", hidden)
	}
	if n := len(visibleWorkers(snap.Workers, now, false)); n != 1 {
		t.Fatalf("visible = %d, want 1", n)
	}
	shown := renderW(t, snap, now, true)
	if !strings.Contains(shown, "off 3m") || !strings.Contains(shown, "peer_eof") || strings.Contains(shown, "(hidden)") {
		t.Fatalf("shown view lacks off reason:\n%s", shown)
	}
	if n := len(visibleWorkers(snap.Workers, now, true)); n != 3 {
		t.Fatalf("visible with off = %d, want 3", n)
	}
}

func TestWorkersShowAllWhilePoolDown(t *testing.T) {
	now := addrNow
	snap := workersFixture(now)
	snap.Pool.Stale = true
	if out := renderW(t, snap, now, false); !strings.Contains(out, "live") || strings.Contains(out, "hidden") {
		t.Fatalf("pool down should list every worker:\n%s", out)
	}
}

func TestMinerViewEffortAndBlocks(t *testing.T) {
	a := fixturegen.Addr(0)
	snap := workersFixture(addrNow)
	snap.Workers[0].BestShare = 5e9
	snap.Round = RoundInfo{Total: 1e11, Miners: []RoundMiner{
		{Address: fixturegen.Addr(1), Diff: 5e10, Pct: 50},
		{Address: a, Diff: 2.5e10, Pct: 25, OwnLuck: 12.5, OwnLuckOK: true, Best: 3e9, BestOfNet: 0.3},
	}}
	snap.Blocks = []BlockRow{
		{Height: 900, Finder: a + ".live", FinderLuck: 40, FinderLuckOK: true},
		{Height: 899, Finder: fixturegen.Addr(1) + ".x", FinderLuck: 99, FinderLuckOK: true},
		{Height: 898, Finder: a + ".gone", FinderLuck: 120, FinderLuckOK: true},
		{Height: 897, Finder: a + ".live"},
	}
	out := renderAddr(t, nil, snap, a)
	t.Log("\n" + out)
	for _, want := range []string{"own effort this round", "12.5%", "share of the round", "25.0%", "blocks found", "3",
		"median finder effort", "80.0%", "over 2 blocks", "#900  effort 40.0%", "#898  effort 120", "#897  effort -", "best"} {
		if !strings.Contains(out, want) {
			t.Errorf("miner view lacks %q", want)
		}
	}
}

func TestMinerViewUnknownIsDash(t *testing.T) {
	a := fixturegen.Addr(0)
	out := renderAddr(t, nil, workersFixture(addrNow), a)
	for _, want := range []string{"own effort this round", "share of the round", "median finder effort"} {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, want) && !strings.Contains(l, " -") {
				t.Errorf("%q should be a dash: %q", want, l)
			}
		}
	}
}
