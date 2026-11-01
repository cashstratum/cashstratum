package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"cashstratumapi/cmd/cashtop/internal/demo"
	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui"
)

// staleProvider serves the demo snapshot marked as a pool that stopped eight
// minutes before fixedNow. Raw values stay as they were.
type staleProvider struct {
	ui.Provider
	reason string
}

func (p staleProvider) Snapshot() ui.Snapshot {
	s := p.Provider.Snapshot()
	s.Pool.Stale = true
	s.Pool.LastActivity = fixedNow.Add(-8 * time.Minute)
	s.Pool.StaleReason = p.reason
	return s
}

func staleModel(caps termcap.Caps, reason string) *ui.Model {
	prov := staleProvider{Provider: demo.New(demo.Options{Seed: 1, Now: fixedNow}), reason: reason}
	return ui.New(prov, ui.Options{Caps: caps, Now: func() time.Time { return fixedNow }})
}

func TestGoldenStaleDashboard(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	out := runTea(t, staleModel(caps, "log idle"))
	checkScreen(t, out, goldenW, goldenH)
	golden.RequireEqual(t, out)
}

func TestStaleBanner(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	out := runTea(t, staleModel(caps, "log idle; no listener on :3333"))
	want := "POOL NOT RUNNING · log idle since 11:52:00 UTC (8m ago) · no listener on :3333"
	if !strings.Contains(out, want) {
		t.Fatalf("banner missing %q:\n%s", want, out)
	}

	ascii := termcap.Detect(envFrom(map[string]string{"TERM": "xterm-256color", "LANG": "C"}), termcap.Flags{})
	out = runTea(t, staleModel(ascii, "log idle"))
	if !strings.Contains(out, "POOL NOT RUNNING | log idle since 11:52:00 UTC (8m ago)") {
		t.Fatalf("ascii banner missing:\n%s", out)
	}
	for _, r := range out {
		if r > 126 {
			t.Fatalf("non-ASCII %q in ascii stale screen", r)
		}
	}
}

func TestStaleWorkersNotLive(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	out := runTea(t, staleModel(caps, "log idle"), "2")
	if strings.Contains(out, "● live") || strings.Contains(out, "◐ stale") {
		t.Fatalf("workers shown live while the pool is down:\n%s", out)
	}
	if !strings.Contains(out, "0 live") || !strings.Contains(out, "○ off") {
		t.Fatalf("workers not off:\n%s", out)
	}
	if strings.Contains(out, "H/s") {
		t.Fatalf("hashrate rendered while stale:\n%s", out)
	}
}

func TestZeroAllAliveRendersDash(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	prov := zeroAliveProvider{demo.New(demo.Options{Seed: 1, Now: fixedNow})}
	out := runTea(t, ui.New(prov, ui.Options{Caps: caps, Now: func() time.Time { return fixedNow }}))
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "all alive") {
			if strings.Contains(l, "0001") || strings.Contains(l, "y ") || !strings.Contains(l, "all alive -") {
				t.Fatalf("unknown streak not a dash: %q", l)
			}
			return
		}
	}
	t.Fatal("no 'all alive' line on the dashboard")
}

type zeroAliveProvider struct{ ui.Provider }

func (p zeroAliveProvider) Snapshot() ui.Snapshot {
	s := p.Provider.Snapshot()
	s.Pool.AllAliveSince = time.Time{}
	s.Pool.LastBlock = time.Time{}
	for i := range s.Blocks {
		s.Blocks[i].Time = time.Time{}
	}
	return s
}
