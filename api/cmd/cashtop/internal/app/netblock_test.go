package app

import (
	"context"
	"testing"
	"time"

	"cashstratumapi/cmd/cashtop/internal/rpc"
	"cashstratumapi/internal/fixturegen"
)

func fakeHeaders(t0 time.Time) func(context.Context, string) (rpc.HeaderDiff, error) {
	hs := map[string]rpc.HeaderDiff{
		"h1": {Hash: "h1", Time: t0.Unix(), Difficulty: 100},
		"h2": {Hash: "h2", Time: t0.Add(9 * time.Minute).Unix(), Difficulty: 101, PreviousHash: "h1"},
		"h3": {Hash: "h3", Time: t0.Add(12 * time.Minute).Unix(), Difficulty: 99, PreviousHash: "h2"},
	}
	return func(_ context.Context, h string) (rpc.HeaderDiff, error) { return hs[h], nil }
}

func setTip(a *App, hash string, height int) {
	a.mu.Lock()
	a.net.RPCOK, a.net.TipHash, a.net.Tip = true, hash, height
	a.mu.Unlock()
}

func TestRefreshTipAnnouncesChangesOnce(t *testing.T) {
	_, dir := tree(t, false)
	a := open(t, dir)
	t0 := fixturegen.FixtureStart
	fetch := fakeHeaders(t0)
	ctx := context.Background()

	setTip(a, "h2", 200)
	if _, ok := a.refreshTipWith(ctx, fetch); ok {
		t.Fatal("first observation must be silent")
	}
	if _, ok := a.refreshTipWith(ctx, fetch); ok {
		t.Fatal("unchanged tip must be silent")
	}

	setTip(a, "h3", 201)
	m, ok := a.refreshTipWith(ctx, fetch)
	if !ok {
		t.Fatal("tip change must be announced")
	}
	if m.Height != 201 || m.Diff != 99 || m.PrevDiff != 101 || m.Gap != 3*time.Minute {
		t.Fatalf("wrong message: %+v", m)
	}
	if _, ok := a.refreshTipWith(ctx, fetch); ok {
		t.Fatal("same tip announced twice")
	}

	var n = a.fillNetBlockProbe()
	if n.PrevDifficulty != 101 || n.Difficulty != 99 {
		t.Fatalf("snapshot fields: %+v", n)
	}
}

func TestLogAnnouncementSuppressesNodeAnnouncement(t *testing.T) {
	_, dir := tree(t, false)
	a := open(t, dir)
	t0 := fixturegen.FixtureStart
	fetch := fakeHeaders(t0)
	ctx := context.Background()

	setTip(a, "h2", 200)
	a.refreshTipWith(ctx, fetch)

	a.noteLogDiff(101, t0.Add(9*time.Minute))
	a.noteLogDiff(99, t0.Add(12*time.Minute))
	m, ok := a.noteNetworkBlock("h3", t0.Add(12*time.Minute))
	if !ok || m.Height != 201 || m.Diff != 99 || m.PrevDiff != 101 {
		t.Fatalf("log announcement wrong: %+v ok=%v", m, ok)
	}
	if _, ok := a.noteNetworkBlock("h3", t0.Add(12*time.Minute)); ok {
		t.Fatal("log announced the same tip twice")
	}
	setTip(a, "h3", 201)
	if _, ok := a.refreshTipWith(ctx, fetch); ok {
		t.Fatal("tip already announced by the log was announced again")
	}
}

// fillNetBlockProbe runs the snapshot merge over the node's current view.
func (a *App) fillNetBlockProbe() (n struct{ Difficulty, PrevDifficulty float64 }) {
	a.mu.Lock()
	ni := a.net
	a.mu.Unlock()
	a.fillNetBlock(&ni, time.Time{}, time.Time{})
	n.Difficulty, n.PrevDifficulty = ni.Difficulty, ni.PrevDifficulty
	return
}
