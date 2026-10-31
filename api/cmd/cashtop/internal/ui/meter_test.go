package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"cashstratumapi/cmd/cashtop/internal/termcap"
)

func testCtx(ascii, motion bool, frame int) *Context {
	caps := termcap.Caps{Glyphs: termcap.UTF8, NoColor: true}
	if ascii {
		caps.Glyphs = termcap.ASCII
	}
	t0 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	snap := &Snapshot{
		Pool: PoolCard{NetworkDiff: 1e12, BestShare: 3.2e9},
		Blocks: []BlockRow{
			{Height: 3, Luck: 40}, {Height: 2, Luck: 0.5}, {Height: 1, Luck: 0},
		},
		Hashrates: []float64{5, 7, 6, 9, 12, 11, 8, 14, 20, 18, 22, 25, 21, 30, 28, 33},
	}
	for i := 0; i < 30; i++ {
		st := []string{"SHARE", "SHARE", "DROP", "BLOCK"}[i%4]
		snap.Feed = append(snap.Feed, FeedRow{Time: t0.Add(time.Duration(i) * time.Second), Style: st, Text: "share " + itoa(i)})
	}
	return &Context{Snap: snap, Now: t0, Frame: frame, Caps: caps, Theme: ThemeFor(caps), Motion: motion && !ascii, sym: symbolsFor(caps)}
}

func TestMeterPos(t *testing.T) {
	if i, ok := meterPos(1e6, 1e12, 61); !ok || i != 30 {
		t.Fatalf("midpoint: %d %v", i, ok)
	}
	if i, _ := meterPos(5e12, 1e12, 61); i != 60 {
		t.Fatalf("above net must clamp to the end, got %d", i)
	}
	if i, ok := meterPos(0.5, 1e12, 61); !ok || i != 0 {
		t.Fatalf("below 1 clamps to start, got %d", i)
	}
	if _, ok := meterPos(10, 0, 61); ok {
		t.Fatal("no network difficulty must not place")
	}
	if _, ok := meterPos(0, 1e12, 61); ok {
		t.Fatal("zero share must not place")
	}
}

func TestEffortPos(t *testing.T) {
	for _, tc := range []struct {
		pct  float64
		want int
	}{{0, 0}, {100, 30}, {150, 45}, {300, 90}, {450, 90}} {
		if i, ok := effortPos(tc.pct, 91); !ok || i != tc.want {
			t.Fatalf("effortPos(%v) = %d %v, want %d", tc.pct, i, ok, tc.want)
		}
	}
	if _, ok := effortPos(-1, 91); ok {
		t.Fatal("negative effort must not place")
	}
	if _, ok := effortPos(10, 0); ok {
		t.Fatal("no cells must not place")
	}
}

func TestEffortTicks(t *testing.T) {
	blocks := []BlockRow{{Luck: 100}, {Luck: 0}, {Luck: 900}, {Luck: 3}}
	got := effortTicks(blocks, 91)
	if len(got) != 3 || got[0] != 30 || got[1] != 90 || got[2] != 1 {
		t.Fatalf("ticks = %v", got)
	}
}

func TestMedianEffort(t *testing.T) {
	if _, n := medianEffort(nil); n != 0 {
		t.Fatal("no blocks")
	}
	if m, n := medianEffort([]BlockRow{{Luck: 50}, {Luck: 0}, {Luck: 10}, {Luck: 400}}); m != 50 || n != 3 {
		t.Fatalf("odd: %v %d", m, n)
	}
	if m, n := medianEffort([]BlockRow{{Luck: 10}, {Luck: 30}}); m != 20 || n != 2 {
		t.Fatalf("even: %v %d", m, n)
	}
}

func TestEffortStyleBands(t *testing.T) {
	th := Cashgreen()
	for _, tc := range []struct {
		pct  float64
		want string
	}{{0, "accent"}, {99.9, "accent"}, {100, "warn"}, {200, "warn"}, {200.1, "bad"}, {500, "bad"}} {
		got := effortStyle(th, tc.pct).Render("x")
		var want string
		switch tc.want {
		case "accent":
			want = th.Accent.Render("x")
		case "warn":
			want = th.Warn.Render("x")
		default:
			want = th.Bad.Render("x")
		}
		if got != want {
			t.Fatalf("pct %v: wrong band", tc.pct)
		}
	}
}

func TestEffortMeterGolden(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ascii  bool
		w      int
		mutate func(*Snapshot)
	}{
		{"UTF8", false, 80, func(s *Snapshot) { s.Pool.RoundPercent = 12.9 }},
		{"Narrow", false, 40, func(s *Snapshot) { s.Pool.RoundPercent = 150 }},
		{"ASCII", true, 80, func(s *Snapshot) { s.Pool.RoundPercent = 250 }},
		{"Over300", false, 80, func(s *Snapshot) { s.Pool.RoundPercent = 412 }},
		{"NoBlocks", false, 80, func(s *Snapshot) { s.Pool.RoundPercent = 12.9; s.Blocks = nil }},
		{"Stale", false, 80, func(s *Snapshot) { s.Pool.RoundPercent = 12.9; s.Pool.Stale = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testCtx(tc.ascii, false, 0)
			tc.mutate(ctx.Snap)
			lines := renderEffortMeter(ctx, tc.w)
			want := 3
			if len(ctx.Snap.Blocks) == 0 {
				want = 2
			}
			if len(lines) != want {
				t.Fatalf("meter has %d lines, want %d", len(lines), want)
			}
			for _, l := range lines {
				if ansi.StringWidth(l) != tc.w {
					t.Fatalf("width %d != %d: %q", ansi.StringWidth(l), tc.w, l)
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

func TestEffortRugStacked(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		ctx := testCtx(ascii, false, 0)
		ctx.Snap.Pool.RoundPercent = 68
		ctx.Snap.Blocks = []BlockRow{{Luck: 100}, {Luck: 100}, {Luck: 100}, {Luck: 50}, {Luck: 150}, {Luck: 150}}
		lines := renderEffortMeter(ctx, 100)
		if len(lines) != 3 {
			t.Fatalf("want 3 lines, got %d", len(lines))
		}
		rug := ansi.Strip(lines[1])
		r := ctx.sym
		if !strings.Contains(rug, r.MeterRamp[2]) || !strings.Contains(rug, r.MeterRamp[1]) || !strings.Contains(rug, r.MeterRamp[0]) {
			t.Fatalf("rug lacks density steps: %q", rug)
		}
		if !strings.Contains(rug, "past blocks (6)") {
			t.Fatalf("rug label: %q", rug)
		}
		if strings.Contains(ansi.Strip(lines[0]), r.MeterRamp[0]) && !ascii {
			t.Fatalf("bar line must carry no ticks: %q", lines[0])
		}
		bar := []rune(ansi.Strip(lines[0]))
		leg := []rune(ansi.Strip(lines[2]))
		due := -1
		for i, c := range bar {
			if string(c) == r.MeterDue {
				due = i
			}
		}
		if due < 1 || string(leg[due-1:due+3]) != "100%" {
			t.Fatalf("100%% not under due marker (%d): %q", due, ansi.Strip(lines[2]))
		}
		if ascii {
			noHighBytes(t, strings.Join(lines, "\n"))
		}
	}
}

func TestMeterGolden(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ascii bool
		w     int
	}{{"UTF8", false, 80}, {"Narrow", false, 40}, {"ASCII", true, 80}} {
		t.Run(tc.name, func(t *testing.T) {
			lines := renderBlockMeter(testCtx(tc.ascii, false, 0), tc.w)
			if len(lines) != 2 {
				t.Fatalf("meter has %d lines", len(lines))
			}
			for _, l := range lines {
				if ansi.StringWidth(l) != tc.w {
					t.Fatalf("width %d != %d: %q", ansi.StringWidth(l), tc.w, l)
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

func noHighBytes(t *testing.T, s string) {
	t.Helper()
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7F {
			t.Fatalf("non-ASCII byte 0x%02x in %q", s[i], s)
		}
	}
}
