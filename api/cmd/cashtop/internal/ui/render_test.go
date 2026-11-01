package ui_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"cashstratumapi/cmd/cashtop/internal/demo"
	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/fixturegen"
)

var (
	escRE      = regexp.MustCompile("\x1b")
	sgrRE      = regexp.MustCompile("\x1b\\[[0-9;:]*m")
	cashAddrRE = regexp.MustCompile(`(?:bitcoincash:)?[qp][a-z0-9]{41}\b`)
	ipRE       = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
)

func frameOptions(th ui.Theme) ui.Options {
	caps := termcap.Detect(utf8Env, termcap.Flags{Quiet: true})
	return ui.Options{Caps: caps, Theme: &th, Now: func() time.Time { return fixedNow }}
}

func showcaseFrames(t *testing.T, th ui.Theme, w, h int) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, v := range ui.Views {
		p := demo.Showcase(demo.Options{Seed: 7, Now: fixedNow})
		f, err := ui.RenderFrame(p, frameOptions(th), v, w, h)
		if err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		out[v] = f
	}
	return out
}

func TestRenderFrameShape(t *testing.T) {
	for _, name := range []string{"cashgreen", "mono"} {
		th, err := ui.ThemeNamed(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{120, 36}, {100, 30}} {
			w, h := size[0], size[1]
			for v, f := range showcaseFrames(t, th, w, h) {
				if strings.TrimSpace(ansi.Strip(f)) == "" {
					t.Fatalf("%s/%s: empty frame", name, v)
				}
				lines := strings.Split(f, "\n")
				if len(lines) != h {
					t.Fatalf("%s/%s: %d lines, want %d", name, v, len(lines), h)
				}
				for i, l := range lines {
					if got := ansi.StringWidth(l); got > w {
						t.Fatalf("%s/%s: line %d is %d cells wide, max %d", name, v, i, got, w)
					}
				}
				if n, m := len(escRE.FindAllString(f, -1)), len(sgrRE.FindAllString(f, -1)); n != m {
					t.Fatalf("%s/%s: %d escapes, only %d are SGR", name, v, n, m)
				}
				if name == "cashgreen" && !strings.Contains(f, "38;2;") {
					t.Fatalf("%s/%s: no TrueColor foreground", name, v)
				}
			}
		}
	}
}

func TestRenderFrameDemoAndErrors(t *testing.T) {
	th, _ := ui.ThemeNamed("cashgreen")
	p := demo.New(demo.Options{Seed: 1, Now: fixedNow})
	for _, v := range ui.Views {
		if _, err := ui.RenderFrame(p, frameOptions(th), v, 120, 36); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	if _, err := ui.RenderFrame(p, frameOptions(th), "nope", 120, 36); err == nil {
		t.Fatal("unknown view must fail")
	}
	if _, err := ui.RenderFrame(p, frameOptions(th), "feed", 0, 36); err == nil {
		t.Fatal("zero width must fail")
	}
	if _, err := ui.ThemeNamed("neon"); err == nil {
		t.Fatal("unknown theme must fail")
	}
}

func TestRenderFrameDeterministic(t *testing.T) {
	th, _ := ui.ThemeNamed("cashgreen")
	a := showcaseFrames(t, th, 120, 36)
	b := showcaseFrames(t, th, 120, 36)
	for v := range a {
		if a[v] != b[v] {
			t.Fatalf("%s differs between runs", v)
		}
	}
}

func TestCelebrationFrameHasFireworks(t *testing.T) {
	th, _ := ui.ThemeNamed("cashgreen")
	f := ansi.Strip(showcaseFrames(t, th, 120, 36)["celebration"])
	if !strings.Contains(f, "BLOCK FOUND") || !strings.ContainsAny(f, "✦✧·") {
		t.Fatal("celebration frame lacks the banner or sparks")
	}
}

// Only synthetic values may appear in public screenshots.
func TestShowcaseFramesCarryOnlySyntheticData(t *testing.T) {
	known := map[string]bool{}
	for i := 0; i < 64; i++ {
		known[fixturegen.Addr(i)] = true
		known[fixturegen.BareAddr(i)] = true
	}
	th, _ := ui.ThemeNamed("cashgreen")
	for _, size := range [][2]int{{120, 36}, {200, 50}} {
		for v, f := range showcaseFrames(t, th, size[0], size[1]) {
			plain := ansi.Strip(f)
			for _, tok := range cashAddrRE.FindAllString(plain, -1) {
				if !known[tok] {
					t.Fatalf("%s: address %q is not a fixturegen value", v, tok)
				}
			}
			for _, ip := range ipRE.FindAllString(plain, -1) {
				if !strings.HasPrefix(ip, "192.0.2.") && !strings.HasPrefix(ip, "198.51.100.") && !strings.HasPrefix(ip, "203.0.113.") {
					t.Fatalf("%s: IP %q is outside the documentation ranges", v, ip)
				}
			}
		}
	}
}

func TestShowcaseSnapshot(t *testing.T) {
	p := demo.Showcase(demo.Options{Seed: 7, Now: fixedNow})
	s := p.Snapshot()
	if hr := s.Pool.Hashrate1m; hr < 4e15 || hr > 6e15 {
		t.Fatalf("pool hashrate %g outside 4-6 PH/s", hr)
	}
	if s.Pool.Users != 5 || len(s.Workers) != 13 || len(s.Blocks) != 30 || len(s.Hashrates) != 60 {
		t.Fatalf("shape: %d users %d workers %d blocks %d points", s.Pool.Users, len(s.Workers), len(s.Blocks), len(s.Hashrates))
	}
	live, stale, idle, off := 0, 0, 0, 0
	for _, w := range s.Workers {
		switch ui.Classify(w, fixedNow) {
		case ui.StateLive:
			live++
		case ui.StateStale:
			stale++
		case ui.StateIdle:
			idle++
		default:
			off++
		}
	}
	if stale < 1 || stale > 2 || idle != 1 || off != 1 || live < 8 {
		t.Fatalf("states live=%d stale=%d idle=%d off=%d", live, stale, idle, off)
	}
	last := s.Blocks[0]
	if s.Pool.LastBlock != last.Time {
		t.Fatal("last block time mismatch")
	}
	minL, maxL := 1e9, 0.0
	for i, b := range s.Blocks {
		minL, maxL = min(minL, b.Luck), max(maxL, b.Luck)
		if i > 0 && (b.Height >= s.Blocks[i-1].Height || !b.Time.Before(s.Blocks[i-1].Time)) {
			t.Fatalf("block %d out of order", i)
		}
		if b.Confirmations != s.Network.Tip-b.Height+1 || b.Reward < 312_500_000 {
			t.Fatalf("block %+v inconsistent", b)
		}
	}
	if minL > 4 || maxL < 300 || fixedNow.Sub(s.Blocks[len(s.Blocks)-1].Time) > 31*24*time.Hour {
		t.Fatalf("efforts %g..%g or span off", minL, maxL)
	}
	if len(s.BestShares) < 5 || s.BestShares[0].Diff >= s.Network.Difficulty {
		t.Fatal("best shares missing or above network difficulty")
	}
	for _, b := range s.BestShares {
		if !b.Time.After(last.Time) {
			t.Fatalf("best share %+v predates the last block", b)
		}
	}
	near := 0
	for _, d := range s.Shares {
		if d.Near {
			near++
		}
	}
	if near == 0 {
		t.Fatal("no near-block shares")
	}
	if age := fixedNow.Sub(s.Network.LastNetBlock); age < 3*time.Minute || age > 5*time.Minute {
		t.Fatalf("last net block %v ago", age)
	}
	if s.Network.PrevDifficulty == s.Network.Difficulty || s.Network.MempoolTx == 0 || s.Network.RPCLatency == 0 {
		t.Fatal("network tab incomplete")
	}
	styles := map[string]bool{}
	for _, r := range s.Feed {
		styles[r.Style] = true
	}
	for _, st := range []string{"CONN", "DROP", "POOL", "AUTH", "NET"} {
		if !styles[st] {
			t.Fatalf("feed lacks %s rows", st)
		}
	}
}
