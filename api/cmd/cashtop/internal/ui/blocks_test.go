package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"cashstratumapi/cmd/cashtop/internal/termcap"
)

func effortCtx() *Context {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	caps := termcap.Caps{Glyphs: termcap.UTF8, NoColor: true}
	return &Context{
		Snap: &Snapshot{
			Pool: PoolCard{NetworkDiff: 1e12, LastBlock: now.Add(-3 * time.Hour)},
			Blocks: []BlockRow{
				{Height: 900, Finder: "qqfinder1.rig", Time: now.Add(-3 * time.Hour), Luck: 80, LuckOK: true, FinderLuck: 12.5, FinderLuckOK: true, Status: BlockConfirmed},
				{Height: 899, Finder: "qqfinder2.rig", Time: now.Add(-9 * time.Hour), Luck: 150, LuckOK: true, Status: BlockPending},
				{Height: 898, Finder: "qqfinder3.rig", Time: now.Add(-20 * time.Hour), FinderLuck: 250, FinderLuckOK: true, Status: BlockConfirmed},
			},
		},
		Now:   now,
		Caps:  caps,
		Theme: Mono(),
		sym:   symbolsFor(caps),
	}
}

func plain(lines []string) string { return ansi.Strip(strings.Join(lines, "\n")) }

func TestBlocksTableFinderAndPoolEffort(t *testing.T) {
	ctx := effortCtx()
	for _, w := range []int{80, 120, 200} {
		out := renderBlocks(ctx, w, 20, 0)
		txt := plain(out)
		if !strings.Contains(txt, "finder") || !strings.Contains(txt, "pool") {
			t.Fatalf("w=%d: header lacks finder/pool columns:\n%s", w, txt)
		}
		for _, l := range out {
			if ansi.StringWidth(l) > w {
				t.Fatalf("w=%d: line %d cells wide: %q", w, ansi.StringWidth(l), l)
			}
		}
		if w == 120 {
			t.Log("\n" + txt)
		}
	}
	txt := plain(renderBlocks(ctx, 200, 20, 0))
	var row899, row898 string
	for _, l := range strings.Split(txt, "\n") {
		if strings.Contains(l, "899") {
			row899 = l
		}
		if strings.Contains(l, "898") {
			row898 = l
		}
	}
	if !strings.Contains(row899, " - ") || !strings.Contains(row899, "150.0%") || strings.Contains(row899, "0%  150") {
		t.Fatalf("unknown finder effort must render a dash beside the pool effort: %q", row899)
	}
	if !strings.Contains(row898, "250.0%") || !strings.Contains(row898, " - ") {
		t.Fatalf("unknown pool effort must render a dash: %q", row898)
	}
}

func TestEffortCellColours(t *testing.T) {
	ctx := effortCtx()
	ctx.Theme = Cashgreen()
	for _, c := range []struct {
		pct  float64
		want lipgloss.Style
	}{{50, ctx.Theme.Accent}, {150, ctx.Theme.Warn}, {250, ctx.Theme.Bad}} {
		b := BlockRow{FinderLuck: c.pct, FinderLuckOK: true}
		if got, want := finderEffort(ctx, b, true), c.want.Render(fmtPct(c.pct)); got != want {
			t.Fatalf("%.0f%%: got %q want %q", c.pct, got, want)
		}
		if got := finderEffort(ctx, b, false); got != fmtPct(c.pct) {
			t.Fatalf("uncoloured cell must be plain, got %q", got)
		}
	}
	if finderEffort(ctx, BlockRow{}, true) != dash || poolEffort(BlockRow{}) != dash {
		t.Fatal("unknown efforts must be a dash")
	}
	if poolEffort(BlockRow{Luck: 0, LuckOK: true}) != "0%" {
		t.Fatal("a known zero effort is not unknown")
	}
}
