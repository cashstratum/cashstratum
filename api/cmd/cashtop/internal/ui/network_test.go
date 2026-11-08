package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func recentSnap(ctx *Context) {
	for i := 0; i < 8; i++ {
		r := NetBlockRow{
			Height: 900000 - i, Hash: "h" + itoa(i), Time: ctx.Now.Add(-time.Duration(i+1) * 10 * time.Minute),
			Interval: 9 * time.Minute, Pool: "AntPool", Tag: "/Mined by AntPool/",
		}
		if i == 2 {
			r.Ours, r.Pool, r.Tag = true, "CashStratum", "/cs/"
		}
		if i == 3 {
			r.Pool, r.Tag = "", ""
		}
		ctx.Snap.Network.Recent = append(ctx.Snap.Network.Recent, r)
	}
	ctx.Snap.Network.DiffSeries = []float64{1e11, 1.1e11, 1.2e11, 1.15e11}
}

func TestNetworkRecentBlocks(t *testing.T) {
	for _, w := range []int{80, 160} {
		ctx := testCtx(false, false, 0)
		recentSnap(ctx)
		lines := renderNetwork(ctx, w, 40)
		plain := ansi.Strip(strings.Join(lines, "\n"))
		for _, want := range []string{"Recent network blocks", "AntPool", "CashStratum (us)", "unknown", "900000"} {
			if !strings.Contains(plain, want) {
				t.Errorf("w=%d missing %q", w, want)
			}
		}
		if !strings.Contains(plain, "/Mined by AntPool/") {
			t.Errorf("w=%d tag column missing", w)
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > w {
				t.Fatalf("w=%d line too wide: %d", w, ansi.StringWidth(l))
			}
		}
	}
}

func TestNetworkWithoutRecentUnchanged(t *testing.T) {
	ctx := testCtx(false, false, 0)
	if strings.Contains(strings.Join(renderNetwork(ctx, 80, 40), "\n"), "Recent network blocks") {
		t.Fatal("no blocks must draw no box")
	}
}
