package ui

import (
	"strings"
	"testing"
	"time"

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

func laneShares(t0 time.Time, counts map[string]int, order []string) []ShareDrop {
	var out []ShareDrop
	for _, a := range order {
		for i := 0; i < counts[a]; i++ {
			out = append(out, ShareDrop{Time: t0.Add(time.Duration(i) * time.Second), Worker: a + ".rig1", Diff: 1e6, SDiff: 2e6, Accepted: i%7 != 3, Near: i%11 == 5})
		}
	}
	return out
}

func TestWorkerAddress(t *testing.T) {
	for in, want := range map[string]string{
		"bitcoincash:qqabc.rig1": "qqabc", "qqabc.rig2": "qqabc", "qqabc": "qqabc",
	} {
		if got := workerAddress(in); got != want {
			t.Errorf("%q: %q want %q", in, got, want)
		}
	}
}

func TestBuildLanesOrderAndOthers(t *testing.T) {
	s := &Snapshot{Shares: laneShares(time.Time{}, map[string]int{"aaa": 3, "bbb": 9, "ccc": 5, "ddd": 1, "eee": 1}, []string{"aaa", "bbb", "ccc", "ddd", "eee"})}
	s.Shares[0].Worker = "bitcoincash:aaa.rig1"
	s.Round = RoundInfo{Miners: []RoundMiner{{Address: "bitcoincash:bbb", Pct: 50}, {Address: "ccc", Pct: 30}}}
	lanes := buildLanes(s, 3)
	if len(lanes) != 3 || lanes[0].addr != "bbb" || lanes[1].addr != "ccc" || !lanes[2].others {
		t.Fatalf("lanes: %+v", lanes)
	}
	if len(lanes[2].drops) != 5 || lanes[2].pct != 20 || !lanes[2].pctOK {
		t.Fatalf("others lane: %+v", lanes[2])
	}
	if !lanes[0].pctOK || lanes[0].pct != 50 {
		t.Fatalf("round pct: %+v", lanes[0])
	}
	if all := buildLanes(s, 10); len(all) != 5 || all[2].addr != "aaa" || all[3].addr != "ddd" {
		t.Fatalf("no-collapse lanes: %+v", all)
	}
	if one := buildLanes(s, 1); len(one) != 1 || !one[0].others || len(one[0].drops) != 19 {
		t.Fatalf("single lane: %+v", one)
	}
	s.Round = RoundInfo{}
	if l := buildLanes(s, 3); l[2].pctOK || l[0].pctOK {
		t.Fatalf("pct without round data: %+v", l)
	}
}

func laneCtx(ascii, motion bool, frame int) *Context {
	ctx := testCtx(ascii, motion, frame)
	t0 := ctx.Now
	ctx.Snap.Shares = laneShares(t0, map[string]int{"qqalpha1111111": 30, "qqbravo2222222": 14, "qqcharlie33333": 6, "qqdelta4444444": 2}, []string{"qqalpha1111111", "qqbravo2222222", "qqcharlie33333", "qqdelta4444444"})
	ctx.Snap.Round = RoundInfo{Total: 100, Miners: []RoundMiner{{Address: "qqalpha1111111", Pct: 62.5}, {Address: "qqbravo2222222", Pct: 30}, {Address: "qqcharlie33333", Pct: 7.5}}}
	return ctx
}

func TestLaneRainRender(t *testing.T) {
	ctx := laneCtx(false, true, 2)
	lines := renderShareRain(ctx, 36, 14)
	out := ansi.Strip(strings.Join(lines, "\n"))
	t.Log("\n" + out)
	if len(lines) > 14 {
		t.Fatalf("%d lines for height 14", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 36 {
			t.Fatalf("line too wide: %q", l)
		}
	}
	for _, want := range []string{"others", "62.5%", "accepted", "rejected", "near miss"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if out != ansi.Strip(strings.Join(renderShareRain(laneCtx(false, true, 2), 36, 14), "\n")) {
		t.Fatal("render is not deterministic")
	}
	if out == ansi.Strip(strings.Join(renderShareRain(laneCtx(false, true, 3), 36, 14), "\n")) {
		t.Fatal("lanes do not move between frames")
	}
}

func TestLaneRainAllAddressesFit(t *testing.T) {
	out := ansi.Strip(strings.Join(renderShareRain(laneCtx(false, true, 0), 60, 14), "\n"))
	if !strings.Contains(out, "lane=address") {
		t.Fatalf("header missing lane hint:\n%s", out)
	}
	if strings.Contains(out, "others") {
		t.Fatalf("4 addresses fit 60 columns, no others lane expected:\n%s", out)
	}
}

func TestLaneRainStaticFallbacks(t *testing.T) {
	for _, tc := range []struct{ ascii, motion bool }{{false, false}, {true, true}} {
		ctx := laneCtx(tc.ascii, tc.motion, 0)
		lines := renderShareRain(ctx, 60, 14)
		out := ansi.Strip(strings.Join(lines, "\n"))
		if len(lines) > 14 || !strings.Contains(out, "accepted") || !strings.Contains(out, "qqdelta4444444.rig1") {
			t.Fatalf("static (%+v):\n%s", tc, out)
		}
		if tc.ascii {
			noHighBytes(t, out)
		}
	}
}

func TestBuildLanesFollowRoundOrder(t *testing.T) {
	// Share-count order is bbb, aaa, ccc; the round says aaa, ccc.
	s := &Snapshot{Shares: laneShares(time.Time{}, map[string]int{"aaa": 3, "bbb": 9, "ccc": 1, "ddd": 5}, []string{"aaa", "bbb", "ccc", "ddd"})}
	s.Round = RoundInfo{Miners: []RoundMiner{{Address: "bitcoincash:aaa", Pct: 70}, {Address: "ccc", Pct: 30}}}
	var got []string
	for _, l := range buildLanes(s, 10) {
		got = append(got, l.addr)
	}
	if strings.Join(got, ",") != "aaa,ccc,bbb,ddd" {
		t.Fatalf("round order: %v", got)
	}
	s.Round = RoundInfo{}
	got = nil
	for _, l := range buildLanes(s, 10) {
		got = append(got, l.addr)
	}
	if strings.Join(got, ",") != "bbb,ddd,aaa,ccc" {
		t.Fatalf("share-count fallback: %v", got)
	}
}

func TestRainHeaderDegradesWholeParts(t *testing.T) {
	ctx := laneCtx(false, true, 0)
	for w, want := range map[int]string{60: "r back", 40: "lane=address", 28: "52 shares", 14: "Share rain"} {
		h := strings.TrimRight(ansi.Strip(rainHeader(ctx, w)), " ")
		if !strings.Contains(h, want) || ansi.StringWidth(h) > w {
			t.Errorf("w=%d: %q lacks %q", w, h, want)
		}
		if w < 60 && strings.Contains(h, "r back") || w < 40 && strings.Contains(h, "lane=") || w < 28 && strings.Contains(h, "shares") {
			t.Errorf("w=%d: kept a part that does not fit: %q", w, h)
		}
	}
}
