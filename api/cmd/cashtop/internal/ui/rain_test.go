package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
)

func TestRainKind(t *testing.T) {
	for style, want := range map[string]rainKind{"BLOCK": rainNear, "drop": rainRejected, "SHARE": rainAccepted, "": rainAccepted} {
		if got := rainKindOf(FeedRow{Style: style}); got != want {
			t.Errorf("%q: %v want %v", style, got, want)
		}
	}
}

func TestRainSampleBounded(t *testing.T) {
	rows := make([]FeedRow, 5000)
	if n := len(rainSample(rows, maxRainDrops)); n != maxRainDrops {
		t.Fatalf("sampled %d", n)
	}
}

func TestRainGolden(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ascii, motion bool
		frame         int
	}{{"Falling0", false, true, 0}, {"Falling3", false, true, 3}, {"StaticReduceMotion", false, false, 0}, {"StaticASCII", true, true, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testCtx(tc.ascii, tc.motion, tc.frame)
			lines := renderShareRain(ctx, 60, 14)
			if len(lines) > 14 {
				t.Fatalf("%d lines for height 14", len(lines))
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > 60 {
					t.Fatalf("line too wide: %q", l)
				}
			}
			out := strings.Join(lines, "\n")
			if tc.ascii {
				noHighBytes(t, out)
			}
			golden.RequireEqual(t, ansi.Strip(out))
		})
	}
}

func TestRainDeterministicAndMoves(t *testing.T) {
	a := strings.Join(renderShareRain(testCtx(false, true, 5), 60, 14), "\n")
	if a != strings.Join(renderShareRain(testCtx(false, true, 5), 60, 14), "\n") {
		t.Fatal("same frame differs")
	}
	if a == strings.Join(renderShareRain(testCtx(false, true, 6), 60, 14), "\n") {
		t.Fatal("rain does not move between frames")
	}
}

func TestRainPrefersShares(t *testing.T) {
	s := &Snapshot{
		Feed:   []FeedRow{{Style: "AUTH", Text: "feed row"}},
		Shares: []ShareDrop{{Accepted: true}, {Accepted: false}, {Accepted: true, Near: true}},
	}
	got := rainDrops(s)
	if len(got) != 3 || got[0].kind != rainAccepted || got[1].kind != rainRejected || got[2].kind != rainNear {
		t.Fatalf("drops from shares: %+v", got)
	}
	s.Shares = nil
	if got := rainDrops(s); len(got) != 1 || got[0].text != "feed row" {
		t.Fatalf("feed fallback: %+v", got)
	}
}
