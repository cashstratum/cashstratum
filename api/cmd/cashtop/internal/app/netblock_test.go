package app

import (
	"encoding/hex"
	"os"
	"path/filepath"

	"cashstratumapi/cmd/cashtop/internal/ui"
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

func TestRefreshRecentWindowAndOurs(t *testing.T) {
	_, dir := tree(t, false)
	a := open(t, dir)
	t0 := fixturegen.FixtureStart
	chain := map[string]rpc.Coinbase{
		"c1": {Hash: "c1", Height: 1, Time: t0.Unix(), Difficulty: 10, ScriptHex: hex.EncodeToString([]byte("/Mined by F2Pool/"))},
		"c2": {Hash: "c2", Height: 2, Time: t0.Add(10 * time.Minute).Unix(), Difficulty: 11, PreviousHash: "c1", ScriptHex: hex.EncodeToString([]byte("/our-sig-9/"))},
		"c3": {Hash: "c3", Height: 3, Time: t0.Add(15 * time.Minute).Unix(), Difficulty: 12, PreviousHash: "c2", ScriptHex: hex.EncodeToString([]byte("/nobody known/"))},
	}
	calls := 0
	fetch := func(_ context.Context, h string) (rpc.Coinbase, error) {
		calls++
		return chain[h], nil
	}
	setTip(a, "c3", 3)
	a.track().mu.Lock()
	a.track().sig, a.track().sigRead = "our-sig-9", true
	a.track().mu.Unlock()
	a.refreshRecentWith(context.Background(), fetch)
	if calls != 3 {
		t.Fatalf("calls %d", calls)
	}
	a.refreshRecentWith(context.Background(), fetch)
	if calls != 3 {
		t.Fatalf("cached blocks refetched: %d", calls)
	}
	var n ui.NetworkInfo
	n.TipHash = "c3"
	a.track().mu.Lock()
	a.fillRecent(&n)
	a.track().mu.Unlock()
	if len(n.Recent) != 3 || n.Recent[0].Hash != "c3" || n.Recent[0].Interval != 5*time.Minute {
		t.Fatalf("rows %+v", n.Recent)
	}
	if !n.Recent[1].Ours || n.Recent[1].Pool != "CashStratum" || n.Recent[0].Ours {
		t.Fatalf("ours detection wrong: %+v", n.Recent)
	}
	if n.Recent[2].Pool != "F2Pool" || n.Recent[0].Pool != "" {
		t.Fatalf("pool names wrong: %+v", n.Recent)
	}
	if len(n.DiffSeries) != 3 || n.DiffSeries[0] != 10 || n.DiffSeries[2] != 12 {
		t.Fatalf("series %v", n.DiffSeries)
	}
}

func TestRefreshRecentBudget(t *testing.T) {
	_, dir := tree(t, false)
	a := open(t, dir)
	calls := 0
	fetch := func(_ context.Context, h string) (rpc.Coinbase, error) {
		calls++
		return rpc.Coinbase{Hash: h, Height: 1, PreviousHash: h + "x"}, nil
	}
	setTip(a, "t", 1)
	a.refreshRecentWith(context.Background(), fetch)
	if calls != recentBudget {
		t.Fatalf("budget not honored: %d", calls)
	}
}

func TestReadBtcSig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	if err := os.WriteFile(p, []byte(`{"btcsig":" /x/ "}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if readBtcSig(p) != "/x/" || readBtcSig("") != "" || readBtcSig(p+"nope") != "" {
		t.Fatal("btcsig read wrong")
	}
}
