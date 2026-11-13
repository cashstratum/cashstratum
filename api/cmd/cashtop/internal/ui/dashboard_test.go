package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func roundFixture(ctx *Context) {
	ctx.Snap.Round = RoundInfo{
		Since: ctx.Now.Add(-3 * time.Hour), Height: 901, Total: 8e10,
		Miners: []RoundMiner{
			{Address: "bitcoincash:qqalpha000000000000000000000000000000x1", Diff: 5e10, Pct: 62.5, OwnLuck: 40, OwnLuckOK: true, Best: 3e9, BestOfNet: 0.3},
			{Address: "qqbravo1111111111111111111111111111111y2", Diff: 2.4e10, Pct: 30, Best: 1e9, BestOfNet: 0.1},
			{Address: "qqcharlie22222222222222222222222222222z3", Diff: 6e9, Pct: 7.5, OwnLuck: 5, OwnLuckOK: true, Best: 5e8, BestOfNet: 0.05},
		},
	}
}

func TestRoundTableOrderAndPercentages(t *testing.T) {
	ctx := effortCtx()
	roundFixture(ctx)
	for _, w := range []int{40, 60, 100} {
		out := roundTable(ctx, w, 10)
		for _, l := range out {
			if ansi.StringWidth(l) > w {
				t.Fatalf("w=%d: line is %d cells: %q", w, ansi.StringWidth(l), l)
			}
		}
	}
	out := plain(roundTable(ctx, 58, 10))
	t.Log("\n" + out)
	a, b, c := strings.Index(out, "62.5%"), strings.Index(out, "30.0%"), strings.Index(out, "7.50%")
	if a < 0 || b < a || c < b {
		t.Fatalf("rows must keep the snapshot order with their percentages:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[2], "30.0%") || !strings.Contains(lines[2], " - ") {
		t.Fatalf("unknown own effort must be a dash: %q", lines[2])
	}
	if !strings.Contains(plain([]string{roundTitle(ctx)}), "(900, 3h 0m ago)") && !strings.Contains(roundTitle(ctx), "(900, 3h") {
		t.Fatalf("title: %q", roundTitle(ctx))
	}
	ctx.Snap.Round = RoundInfo{}
	if got := plain(roundTable(ctx, 58, 10)); !strings.Contains(got, "no shares since the last block yet") {
		t.Fatalf("empty round: %q", got)
	}
}

func TestPoolBoxRows(t *testing.T) {
	ctx := effortCtx()
	roundFixture(ctx)
	txt := plain(renderDashboard(ctx, 120, 40))
	for _, want := range []string{"last pool block", "3h 0m ago", "top round", "62.5%"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("pool box lacks %q:\n%s", want, txt)
		}
	}
	for _, gone := range []string{"all alive", "longest", "Best shares since last block"} {
		if strings.Contains(txt, gone) {
			t.Fatalf("pool box still shows %q", gone)
		}
	}
	ctx.Snap.Round = RoundInfo{}
	ctx.Snap.Pool.LastBlock = time.Time{}
	txt = plain(renderDashboard(ctx, 120, 40))
	if !strings.Contains(txt, "top round") {
		t.Fatal("row must remain when unknown")
	}
	t.Log("\n" + plain(renderDashboard(effortCtxWithRound(), 120, 40)))
}

func effortCtxWithRound() *Context {
	c := effortCtx()
	roundFixture(c)
	return c
}

func TestRecentBlocksWidths(t *testing.T) {
	ctx := effortCtx()
	for _, w := range []int{30, 38, 58, 98} {
		out := recentBlocks(ctx, w, 10, dash)
		for _, l := range out {
			if ansi.StringWidth(l) > w {
				t.Fatalf("w=%d: line is %d cells: %q", w, ansi.StringWidth(l), l)
			}
		}
		txt := plain(out)
		if !strings.Contains(txt, "finder") || !strings.Contains(txt, "pool") {
			t.Fatalf("w=%d: effort columns missing:\n%s", w, txt)
		}
		if w == 58 {
			t.Log("\n" + txt)
		}
	}
}

func TestOverlayEffortLine(t *testing.T) {
	ctx := celebCtx(false, false)
	c := &celebration{}
	c.Update(celebBlock(), ctx)
	if out := ansi.Strip(c.Render(ctx, 120, 30)); !strings.Contains(out, "finder 41.5%   pool 63.2%") {
		t.Fatalf("overlay lacks the effort line:\n%s", out)
	}
	// Pool effort unknown until the update lands, then known.
	m := celebBlock()
	m.Block.Luck, m.Block.LuckOK, m.Block.FinderLuckOK = 0, false, false
	c = &celebration{}
	c.Update(m, ctx)
	if out := ansi.Strip(c.Render(ctx, 120, 30)); !strings.Contains(out, "finder -   pool -") {
		t.Fatalf("unknown efforts must be dashes:\n%s", out)
	}
	c.Update(BlockUpdateMsg{Height: m.Block.Height, Luck: 77}, ctx)
	if !c.block.LuckOK || c.block.Luck != 77 {
		t.Fatal("update must set LuckOK")
	}
	out := ansi.Strip(c.Render(ctx, 120, 30))
	if !strings.Contains(out, "finder -   pool 77.0%") {
		t.Fatalf("after update:\n%s", out)
	}
	t.Log("\n" + out)
}

func propFixture(ctx *Context) {
	roundFixture(ctx)
	ctx.Snap.Prop = PropInfo{On: true, Reward: 312_500_000, MinersPot: 150_000_000, FinderBonus: 156_250_000}
	m := ctx.Snap.Round.Miners
	m[0].EstSats, m[0].Eligible = 93_750_000, true
	m[1].EstSats, m[1].Eligible = 45_000_000, true
	m[2].Eligible = false
}

func TestPropRoundTable(t *testing.T) {
	ctx := effortCtx()
	propFixture(ctx)
	out := plain(roundTable(ctx, 90, 10))
	for _, want := range []string{"est. reward", "0.93750000", "0.45000000", "<1%", "yes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("PROP table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "effort") {
		t.Fatalf("the solo effort column must be gone:\n%s", out)
	}
	for _, l := range roundTable(ctx, 60, 10) {
		if ansi.StringWidth(l) > 60 {
			t.Fatalf("line wider than 60: %q", l)
		}
	}
	ctx.Snap.Prop.Reward = 0
	if got := plain(roundTable(ctx, 90, 10)); strings.Contains(got, "0.9375") || !strings.Contains(got, "est. reward") {
		t.Fatalf("unknown reward must render dashes:\n%s", got)
	}
}

func TestPropDashboardAndAddress(t *testing.T) {
	ctx := effortCtx()
	propFixture(ctx)
	txt := plain(renderDashboard(ctx, 120, 40))
	for _, want := range []string{"block reward", "3.12500000 BCH", "finder bonus", "1.56250000 BCH", "pool work"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("dashboard lacks %q:\n%s", want, txt)
		}
	}
	addr := ctx.Snap.Round.Miners[2].Address
	sec := plain(minerSection(ctx, addr))
	if !strings.Contains(sec, "est. reward") || !strings.Contains(sec, "under 1%") {
		t.Fatalf("address view:\n%s", sec)
	}
	if strings.Contains(sec, "own effort") || strings.Contains(sec, "median finder effort") {
		t.Fatalf("solo figures must be hidden:\n%s", sec)
	}
	ctx.Snap.Prop = PropInfo{}
	solo := plain(minerSection(ctx, addr))
	if !strings.Contains(solo, "own effort this round") || !strings.Contains(solo, "median finder effort") || strings.Contains(solo, "est. reward") {
		t.Fatalf("solo view changed:\n%s", solo)
	}
}

// The PROP help states the miners' pot the pool conf configures, and only a
// generic phrase when it is unknown.
func TestPropHelpShowsMinersPot(t *testing.T) {
	ctx := effortCtx()
	for _, c := range []struct {
		bp   int64
		want string
	}{
		{4600, "miners' pot (46% of the coinbase)"},
		{4800, "miners' pot (48% of the coinbase)"},
		{4650, "miners' pot (46.5% of the coinbase)"},
		{0, "share of the miners' pot, in BCH"},
	} {
		ctx.Snap.Prop = PropInfo{On: true, MinersPotBP: c.bp}
		if out := plain(helpLines(ctx)); !strings.Contains(out, c.want) {
			t.Errorf("bp %d: help is missing %q:\n%s", c.bp, c.want, out)
		}
	}
}
