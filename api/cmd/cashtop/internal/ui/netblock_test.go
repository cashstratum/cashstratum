package ui_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"cashstratumapi/cmd/cashtop/internal/demo"
	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui"
)

func TestNetBlockAgeLevel(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want ui.AgeLevel
	}{
		{0, ui.AgeNormal},
		{ui.NetBlockAmber - time.Second, ui.AgeNormal},
		{ui.NetBlockAmber, ui.AgeAmber},
		{ui.NetBlockRed - time.Second, ui.AgeAmber},
		{ui.NetBlockRed, ui.AgeRed},
		{time.Hour, ui.AgeRed},
	}
	for _, c := range cases {
		if got := ui.NetBlockAgeLevel(c.age); got != c.want {
			t.Errorf("age %v: got %v, want %v", c.age, got, c.want)
		}
	}
}

func TestDiffChange(t *testing.T) {
	cases := []struct {
		prev, cur float64
		pct       float64
		dir       ui.DiffDir
		ok        bool
	}{
		{100, 100.82, 0.82, ui.DiffUp, true},
		{100, 98.9, -1.1, ui.DiffDown, true},
		{100, 100, 0, ui.DiffSame, true},
		{100, 100.001, 0, ui.DiffSame, true},
		{0, 100, 0, ui.DiffSame, false},
		{100, 0, 0, ui.DiffSame, false},
		{100, math.NaN(), 0, ui.DiffSame, false},
	}
	for _, c := range cases {
		pct, dir, ok := ui.DiffChange(c.prev, c.cur)
		if ok != c.ok || dir != c.dir || math.Abs(pct-c.pct) > 1e-9 {
			t.Errorf("DiffChange(%v,%v) = %v,%v,%v; want %v,%v,%v", c.prev, c.cur, pct, dir, ok, c.pct, c.dir, c.ok)
		}
	}
}

// netProvider serves the demo snapshot with network-block fields set.
type netProvider struct {
	ui.Provider
	last       time.Time
	diff, prev float64
}

func (p netProvider) Snapshot() ui.Snapshot {
	s := p.Provider.Snapshot()
	s.Network.LastNetBlock = p.last
	s.Network.Difficulty = p.diff
	s.Network.PrevDifficulty = p.prev
	return s
}

func netModel(caps termcap.Caps, last time.Time, diff, prev float64, now func() time.Time) *ui.Model {
	prov := netProvider{Provider: demo.New(demo.Options{Seed: 1, Now: fixedNow}), last: last, diff: diff, prev: prev}
	return ui.New(prov, ui.Options{Caps: caps, Now: now})
}

func fixed() time.Time { return fixedNow }

func TestGoldenDashboardNetBlockRed(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	m := netModel(caps, fixedNow.Add(-(11*time.Minute + 5*time.Second)), 529e9, 524.7e9, fixed)
	out := runTea(t, m)
	checkScreen(t, out, goldenW, goldenH)
	for _, want := range []string{"last net block", "11m 5s ago", "529G ▲ 0.82%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	golden.RequireEqual(t, out)
}

func TestGoldenDashboardNetBlockNormal(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	m := netModel(caps, fixedNow.Add(-47*time.Second), 529e9, 534.9e9, fixed)
	out := runTea(t, m)
	checkScreen(t, out, goldenW, goldenH)
	for _, want := range []string{"47s ago", "529G ▼ 1.10%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	golden.RequireEqual(t, out)
}

func TestNetBlockUnknownAndEqual(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	out := runTea(t, netModel(caps, time.Time{}, 529e9, 529e9, fixed))
	if !strings.Contains(out, "last net block") || !strings.Contains(out, "529G") || strings.Contains(out, "▲") || strings.Contains(out, "▼") {
		t.Fatalf("unknown/equal render wrong:\n%s", out)
	}
}

func TestNetBlockAgeTicks(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	now := fixedNow
	m := netModel(caps, fixedNow.Add(-47*time.Second), 529e9, 529e9, func() time.Time { return now })
	drive(m, goldenW, goldenH)
	if out := ansi.Strip(m.Render()); !strings.Contains(out, "47s ago") {
		t.Fatalf("want 47s ago:\n%s", out)
	}
	now = now.Add(3 * time.Second)
	if out := ansi.Strip(m.Render()); !strings.Contains(out, "50s ago") {
		t.Fatalf("want 50s ago after clock advance:\n%s", out)
	}
}

func TestNetBlockBanner(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	now := fixedNow
	m := netModel(caps, fixedNow, 529e9, 524.7e9, func() time.Time { return now })
	drive(m, goldenW, goldenH)
	if strings.Contains(ansi.Strip(m.Render()), "NEW BLOCK") {
		t.Fatal("banner before any block")
	}
	m.Update(ui.NetworkBlockMsg{Height: 975170, Time: now, Diff: 529e9, PrevDiff: 524.7e9, Gap: 9*time.Minute + 41*time.Second})
	out := ansi.Strip(m.Render())
	checkScreen(t, out, goldenW, goldenH)
	want := "NEW BLOCK 975170 · diff ▲0.8% · 9m 41s after previous"
	if !strings.Contains(out, want) {
		t.Fatalf("banner missing %q:\n%s", want, out)
	}
	if lines := strings.Split(out, "\n"); !strings.Contains(lines[1], "NEW BLOCK") {
		t.Fatalf("banner not under the header: %q", lines[1])
	}
	now = now.Add(ui.NetBlockBannerFor - time.Millisecond)
	if !strings.Contains(ansi.Strip(m.Render()), "NEW BLOCK") {
		t.Fatal("banner gone early")
	}
	now = now.Add(time.Millisecond)
	out = ansi.Strip(m.Render())
	checkScreen(t, out, goldenW, goldenH)
	if strings.Contains(out, "NEW BLOCK") {
		t.Fatal("banner still shown after its duration")
	}
}

func TestNetBlockBannerASCII(t *testing.T) {
	ascii := termcap.Detect(envFrom(map[string]string{"TERM": "xterm-256color", "LANG": "C"}), termcap.Flags{})
	m := netModel(ascii, fixedNow.Add(-time.Minute), 529e9, 524.7e9, fixed)
	drive(m, goldenW, goldenH)
	m.Update(ui.NetworkBlockMsg{Height: 975170, Time: fixedNow, Diff: 529e9, PrevDiff: 524.7e9, Gap: 9 * time.Minute})
	out := ansi.Strip(m.Render())
	if !strings.Contains(out, "NEW BLOCK 975170 | diff ^0.8%") || !strings.Contains(out, "529G ^ 0.82%") {
		t.Fatalf("ascii markers missing:\n%s", out)
	}
	for i, r := range []byte(out) {
		if r > 0x7F {
			t.Fatalf("non-ASCII byte at %d", i)
		}
	}
}

func TestNetBlockBannerYieldsToCelebration(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	m := netModel(caps, fixedNow, 529e9, 529e9, fixed)
	drive(m, goldenW, goldenH)
	m.Update(ui.BlockFoundMsg{Block: ui.BlockRow{Height: 975171, Finder: "w.rig", Status: ui.BlockPending, Time: fixedNow}})
	m.Update(ui.NetworkBlockMsg{Height: 975171, Time: fixedNow})
	m.Update(key("esc")) // dismiss the celebration
	if strings.Contains(ansi.Strip(m.Render()), "NEW BLOCK") {
		t.Fatal("banner shown for a block already celebrated")
	}
}

func TestNetBlockFlashHonoursReduceMotion(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	const selected = "48;2;22;48;42" // the Selected style's background
	render := func(motion bool, after time.Duration) string {
		now := fixedNow
		prov := netProvider{Provider: demo.New(demo.Options{Seed: 1, Now: fixedNow}), diff: 529e9, prev: 529e9}
		m := ui.New(prov, ui.Options{Caps: caps, Motion: motion, Now: func() time.Time { return now }})
		drive(m, goldenW, goldenH)
		m.Update(ui.NetworkBlockMsg{Height: 1, Time: now})
		now = now.Add(after)
		return m.Render()
	}
	if !strings.Contains(render(true, 0), selected) {
		t.Fatal("flash expected with motion on")
	}
	if strings.Contains(render(true, ui.NetBlockFlashFor), selected) {
		t.Fatal("flash should end after its duration")
	}
	out := render(false, 0)
	if strings.Contains(out, selected) {
		t.Fatal("no flash expected under reduce-motion")
	}
	if !strings.Contains(ansi.Strip(out), "NEW BLOCK") {
		t.Fatal("banner expected under reduce-motion")
	}
}
