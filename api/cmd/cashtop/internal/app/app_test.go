package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"cashstratumapi/cmd/cashtop/internal/index"
	"cashstratumapi/cmd/cashtop/internal/rpc"
	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/poolfmt"
)

func tree(t *testing.T, solves bool) (*fixturegen.Tree, string) {
	t.Helper()
	dir := t.TempDir()
	tr, err := fixturegen.WriteTree(dir, 7, solves)
	if err != nil {
		t.Fatal(err)
	}
	return tr, dir
}

func open(t *testing.T, dir string) *App {
	t.Helper()
	a, err := Open(Config{Root: dir, Memory: true, NoNode: true, Now: func() time.Time {
		return fixturegen.FixtureStart.Add(41 * time.Minute)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func appendLog(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "")); err != nil {
		t.Fatal(err)
	}
}

func count[T any](msgs []tea.Msg) (n int) {
	for _, m := range msgs {
		if _, ok := m.(T); ok {
			n++
		}
	}
	return n
}

func TestSnapshotReflectsTree(t *testing.T) {
	tr, dir := tree(t, true)
	s := open(t, dir).Snapshot()
	if len(s.Workers) == 0 || len(s.Workers)%len(tr.Users) != 0 {
		t.Fatalf("workers = %d for %d users", len(s.Workers), len(tr.Users))
	}
	if s.Pool.Hashrate1m <= 0 || s.Pool.Users <= 0 || s.Pool.NetworkDiff <= 0 {
		t.Fatalf("pool card empty: %+v", s.Pool)
	}
	heights := map[int]bool{}
	for _, b := range s.Blocks {
		heights[b.Height] = true
		if b.Proto != "SV1" && b.Proto != "SV2" {
			t.Errorf("proto %q", b.Proto)
		}
	}
	for _, f := range tr.Found {
		if !heights[f.Height] {
			t.Errorf("found block %d missing from the snapshot", f.Height)
		}
	}
	if len(s.Feed) == 0 {
		t.Fatal("feed backlog empty")
	}
	agents := 0
	for _, w := range s.Workers {
		if w.UserAgent != "" {
			agents++
		}
	}
	if agents == 0 {
		t.Fatal("no useragent taken from the sharelogs")
	}
}

func TestStartupHistoryNeverCelebrates(t *testing.T) {
	tr, dir := tree(t, true)
	if len(tr.Found) == 0 {
		t.Fatal("fixture has no past solves")
	}
	a := open(t, dir)
	for i := 0; i < 3; i++ {
		if n := count[ui.BlockFoundMsg](a.Step()); n != 0 {
			t.Fatalf("step %d: %d BlockFoundMsg for past solves", i, n)
		}
	}
}

func TestAppendedBlockFoundOnce(t *testing.T) {
	tr, dir := tree(t, false)
	a := open(t, dir)
	a.Step()

	top := 0
	for _, f := range tr.Found {
		top = max(top, f.Height)
	}
	g := fixturegen.New(99, fixturegen.FixtureStart.Add(45*time.Minute))
	height := top + 5
	cluster := g.BlockCluster(height, fixturegen.FullWorker(1, 0), "SV1")
	appendLog(t, tr.LogPath, cluster)

	msgs := a.Step()
	if n := count[ui.BlockFoundMsg](msgs); n != 1 {
		t.Fatalf("BlockFoundMsg = %d, want 1", n)
	}
	for _, m := range msgs {
		if b, ok := m.(ui.BlockFoundMsg); ok && (b.Block.Height != height || b.Replay) {
			t.Fatalf("wrong block %+v", b)
		}
	}
	if count[ui.FeedRowsMsg](msgs) == 0 {
		t.Fatal("no feed rows for the cluster")
	}
	if n := count[ui.BlockFoundMsg](a.Step()); n != 0 {
		t.Fatalf("second step: %d", n)
	}
	appendLog(t, tr.LogPath, cluster) // the same lines again
	if n := count[ui.BlockFoundMsg](a.Step()); n != 0 {
		t.Fatalf("duplicate cluster celebrated %d times", n)
	}

	// A restart on the grown log announces nothing.
	a.Close()
	b := open(t, dir)
	if n := count[ui.BlockFoundMsg](b.Step()); n != 0 {
		t.Fatalf("restart celebrated %d", n)
	}
	if rb := ui.ReplayBlock(b.Snapshot(), time.Now()); rb.Height != height {
		t.Fatalf("replay block = %d, want %d", rb.Height, height)
	}
}

// A message the daemon split at 510 bytes is rejoined before it reaches
// the events engine: a solve line whose finder lands in the continuation
// still announces exactly once, with the full finder.
func TestSplitSolveLineRejoined(t *testing.T) {
	tr, dir := tree(t, false)
	a := open(t, dir)
	a.Step()
	top := 0
	for _, f := range tr.Found {
		top = max(top, f.Height)
	}
	height := top + 9
	finder := fixturegen.FullWorker(2, 1) + "." + strings.Repeat("x", 520)
	stamp := fixturegen.Stamp(fixturegen.FixtureStart.Add(50 * time.Minute))
	msg := "Solved and confirmed block " + itoa(height) + " by " + finder
	lines := []string{stamp + msg[:510] + "\n", stamp + msg[510:] + "\n"}
	appendLog(t, tr.LogPath, lines)
	var got []ui.BlockFoundMsg
	for i := 0; i < 3; i++ { // the last chunk is released by an idle poll
		for _, m := range a.Step() {
			if b, ok := m.(ui.BlockFoundMsg); ok {
				got = append(got, b)
			}
		}
	}
	if len(got) != 1 || got[0].Block.Height != height || got[0].Block.Finder != finder {
		t.Fatalf("got %+v", got)
	}
}

// A solve on the very first line after startup is live, not history.
func TestSolveOnFirstNewLine(t *testing.T) {
	tr, dir := tree(t, false)
	a := open(t, dir)
	stamp := fixturegen.Stamp(fixturegen.FixtureStart.Add(50 * time.Minute))
	appendLog(t, tr.LogPath, []string{stamp + "Solved and confirmed block 990001 by " + fixturegen.FullWorker(0, 0) + "\n"})
	if n := count[ui.BlockFoundMsg](a.Step()); n != 1 {
		t.Fatalf("BlockFoundMsg = %d, want 1", n)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestSharesAndAddress(t *testing.T) {
	tr, dir := tree(t, false)
	a := open(t, dir)
	s := a.Snapshot()
	if len(s.Shares) == 0 || len(s.Shares) > ui.MaxShareDrops {
		t.Fatalf("shares = %d", len(s.Shares))
	}
	for i := 1; i < len(s.Shares); i++ {
		if s.Shares[i].Time.Before(s.Shares[i-1].Time) {
			t.Fatal("shares not oldest first")
		}
	}
	d := a.Address(tr.Users[0])
	if !d.LogShares || d.Height <= 0 || len(d.Submits) == 0 || d.Work <= 0 || len(d.LastShare) == 0 {
		t.Fatalf("address data: height %d, %d submits, work %v, %d last", d.Height, len(d.Submits), d.Work, len(d.LastShare))
	}
	for _, sh := range d.Submits {
		if index.PayoutAddress(sh) != poolfmt.NormalizeAddress(tr.Users[0]) {
			t.Fatalf("submit of another address: %s", index.PayoutAddress(sh))
		}
	}
}

func TestDrillDownWinningShare(t *testing.T) {
	tr, dir := tree(t, false)
	a := open(t, dir)
	var hit bool
	for _, f := range tr.Found {
		d, err := a.DrillDown(f.Height)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Jobs) == 0 {
			continue
		}
		hit = true
		wins := 0
		for _, j := range d.Jobs {
			for _, s := range j.Shares {
				if s.Winning {
					wins++
				}
			}
		}
		if wins == 0 {
			t.Fatalf("height %d: no winning share", f.Height)
		}
	}
	if !hit {
		t.Skip("fixture has no sharelog for a found height")
	}
	if d, err := a.DrillDown(1); err != nil || len(d.Jobs) != 0 || d.Dir != "00000001" {
		t.Fatalf("missing dir: %+v %v", d, err)
	}
}

func TestTornPoolStatusKeepsLastGood(t *testing.T) {
	_, dir := tree(t, false)
	a := open(t, dir)
	before := a.Snapshot().Pool
	if err := os.WriteFile(filepath.Join(dir, "logs", "pool", "pool.status"), []byte("{\"runtime\": 12"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshFiles()
	after := a.Snapshot().Pool
	if after.Hashrate1m != before.Hashrate1m || after.Runtime != before.Runtime || after.Hashrate1m == 0 {
		t.Fatalf("torn read replaced values: %+v -> %+v", before, after)
	}
}

func TestNoSharelogsHint(t *testing.T) {
	_, dir := tree(t, false)
	ents, _ := os.ReadDir(filepath.Join(dir, "logs"))
	for _, e := range ents {
		if e.IsDir() && len(e.Name()) == 8 {
			if err := os.RemoveAll(filepath.Join(dir, "logs", e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	s := open(t, dir).Snapshot()
	last := s.Feed[len(s.Feed)-1]
	if last.Style != "HINT" || !strings.Contains(last.Text, "logshares") {
		t.Fatalf("no logshares hint: %+v", last)
	}
}

func TestResolveRoot(t *testing.T) {
	_, dir := tree(t, false)
	conf := filepath.Join(dir, "cashstratum.conf")
	if err := os.WriteFile(conf, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	r, c, err := ResolveRoot(dir, "", env(nil))
	if err != nil || r != dir || c != conf {
		t.Fatalf("--root: %q %q %v", r, c, err)
	}
	r, _, err = ResolveRoot("", conf, env(nil))
	if err != nil || r != dir {
		t.Fatalf("--conf: %q %v", r, err)
	}
	r, _, err = ResolveRoot("", "", env(map[string]string{"CASHSTRATUM_LOG_PATH": filepath.Join(dir, "logs", "cashstratum.log")}))
	if err != nil || r != dir {
		t.Fatalf("env: %q %v", r, err)
	}
	home := t.TempDir()
	if err := os.Rename(dir, filepath.Join(home, "cashstratum")); err != nil {
		t.Fatal(err)
	}
	r, _, err = ResolveRoot("", "", env(map[string]string{"HOME": home}))
	if err != nil || r != filepath.Join(home, "cashstratum") {
		t.Fatalf("home default: %q %v", r, err)
	}
	if _, _, err := ResolveRoot("", "", env(map[string]string{"HOME": t.TempDir()})); err != ErrNoRoot {
		t.Fatalf("want ErrNoRoot, got %v", err)
	}
}

// A pool whose long lines are split at 510 bytes reads exactly like one whose
// lines are whole: same blocks, same best shares (from the User lines), same
// feed; and a split block cluster appended live announces once.
func TestSplitTreeMatchesWholeLines(t *testing.T) {
	whole, split := t.TempDir(), t.TempDir()
	if _, err := fixturegen.WriteTreeOpts(whole, 7, true, fixturegen.TreeOpts{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	tr, err := fixturegen.WriteTreeOpts(split, 7, true, fixturegen.TreeOpts{Verbose: true, Split: true})
	if err != nil {
		t.Fatal(err)
	}
	lines := func(dir string) int {
		b, _ := os.ReadFile(filepath.Join(dir, "logs", "cashstratum.log"))
		return strings.Count(string(b), "\n")
	}
	if lines(split) <= lines(whole) {
		t.Fatalf("split tree has no split lines (%d vs %d)", lines(split), lines(whole))
	}
	// Without sharelogs the leaderboard falls back to the User lines.
	for _, dir := range []string{whole, split} {
		ents, _ := os.ReadDir(filepath.Join(dir, "logs"))
		for _, e := range ents {
			if e.IsDir() {
				os.RemoveAll(filepath.Join(dir, "logs", e.Name()))
			}
		}
	}
	sw, ss := open(t, whole).Snapshot(), open(t, split).Snapshot()
	if len(sw.Blocks) == 0 || len(sw.Blocks) != len(ss.Blocks) {
		t.Fatalf("blocks %d vs %d", len(sw.Blocks), len(ss.Blocks))
	}
	for i := range sw.Blocks {
		w, s := sw.Blocks[i], ss.Blocks[i]
		if w.Height != s.Height || w.Finder != s.Finder || w.Proto != s.Proto || w.Status != s.Status {
			t.Fatalf("block %d: %+v vs %+v", i, w, s)
		}
	}
	if len(sw.BestShares) == 0 || len(sw.BestShares) != len(ss.BestShares) {
		t.Fatalf("best shares %d vs %d", len(sw.BestShares), len(ss.BestShares))
	}
	for i := range sw.BestShares {
		if sw.BestShares[i].Worker != ss.BestShares[i].Worker || sw.BestShares[i].Diff != ss.BestShares[i].Diff {
			t.Fatalf("best share %d: %+v vs %+v", i, sw.BestShares[i], ss.BestShares[i])
		}
	}
	// The feed backlog is a byte window, so the split log (more bytes per
	// message) shows fewer rows; the newest rows must match one for one.
	n := min(len(sw.Feed), len(ss.Feed))
	if n < 100 {
		t.Fatalf("feed rows %d vs %d", len(sw.Feed), len(ss.Feed))
	}
	for i := 1; i <= n-2; i++ { // the oldest row of a window may be a cut message
		if a, b := sw.Feed[len(sw.Feed)-i].Text, ss.Feed[len(ss.Feed)-i].Text; a != b {
			t.Fatalf("feed row -%d: %q vs %q", i, a, b)
		}
	}

	// Live: a verbose User line and a solve whose finder runs past 510
	// bytes, both written in pieces.
	a := open(t, split)
	a.Step()
	top := 0
	for _, f := range tr.Found {
		top = max(top, f.Height)
	}
	g := fixturegen.New(99, fixturegen.FixtureStart.Add(50*time.Minute))
	g.SetVerbose(true)
	finder := fixturegen.FullWorker(1, 0) + "." + strings.Repeat("r", 500)
	whole2 := append([]string{g.User(1)}, g.BlockCluster(top+3, finder, "SV2")...)
	var pieces []string
	for _, l := range whole2 {
		pieces = append(pieces, fixturegen.SplitLine(l)...)
	}
	if len(pieces) <= len(whole2) {
		t.Fatal("appended lines were not split")
	}
	appendLog(t, tr.LogPath, pieces)
	var got []ui.BlockFoundMsg
	for i := 0; i < 3; i++ {
		for _, m := range a.Step() {
			if b, ok := m.(ui.BlockFoundMsg); ok {
				got = append(got, b)
			}
		}
	}
	if len(got) != 1 || got[0].Block.Height != top+3 || got[0].Block.Finder != finder || got[0].Block.Proto != "SV2" {
		t.Fatalf("got %+v", got)
	}

	// The User line rejoins to the same stats the whole line parses to.
	var j poolfmt.Joiner
	var joined []poolfmt.Joined
	for i, p := range fixturegen.SplitLine(whole2[0]) {
		joined = append(joined, j.Feed(strings.TrimSuffix(p, "\n"), int64(i))...)
	}
	joined = append(joined, j.Flush()...)
	want := poolfmt.ParseLine(strings.TrimSuffix(whole2[0], "\n"))
	if len(joined) != 1 || want.Kind != poolfmt.KindUser {
		t.Fatalf("joined %d lines, kind %v", len(joined), want.Kind)
	}
	if gotEv := poolfmt.ParseLine(joined[0].Line); gotEv.Kind != poolfmt.KindUser ||
		gotEv.Stats.Hashrate1m != want.Stats.Hashrate1m || gotEv.Stats.BestShare != want.Stats.BestShare ||
		gotEv.Stats.Reject.Rate1m != want.Stats.Reject.Rate1m || !want.Stats.Reject.Rate1m.OK {
		t.Fatalf("user stats differ after rejoin: %+v vs %+v", gotEv.Stats, want.Stats)
	}
}

// Live address data credits the username, never the connection IP, and
// takes the work from the index once it has seen the address.
func TestAddressKeyedByPayoutNotIP(t *testing.T) {
	_, dir := tree(t, false)
	h, _ := newestShareDir(filepath.Join(dir, "logs"))
	nd := filepath.Join(dir, "logs", fmt.Sprintf("%08x", h+1))
	if err := os.MkdirAll(nd, 0o755); err != nil {
		t.Fatal(err)
	}
	payout, ip := fixturegen.Addr(3), fixturegen.IP(4)
	line := fmt.Sprintf(`{"workinfoid":1,"diff":300,"sdiff":400,"result":true,"createdate":"%d,0","workername":"%s.rig01","username":"%s","address":"%s"}`+"\n",
		fixturegen.FixtureStart.Unix()+60, fixturegen.BareAddr(3), payout, ip)
	if err := os.WriteFile(filepath.Join(nd, "0000000000000001.sharelog"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, dir)
	if err := a.ix.Sync(); err != nil {
		t.Fatal(err)
	}
	d := a.Address(payout)
	if d.Height != h+1 || d.Work != 300 || len(d.Submits) != 1 {
		t.Fatalf("payout: height %d work %v submits %d", d.Height, d.Work, len(d.Submits))
	}
	if d := a.Address(ip); d.Work != 0 || len(d.Submits) != 0 {
		t.Fatalf("IP credited: work %v submits %d", d.Work, len(d.Submits))
	}
}

func openAt(t *testing.T, dir string, cfg Config, now time.Time) *App {
	t.Helper()
	cfg.Root, cfg.Memory, cfg.NoNode = dir, true, true
	cfg.Now = func() time.Time { return now }
	a, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func TestStaleFollowsLogAge(t *testing.T) {
	tr, dir := tree(t, false)
	old := fixturegen.FixtureStart
	if err := os.Chtimes(tr.LogPath, old, old); err != nil {
		t.Fatal(err)
	}
	probe := openAt(t, dir, Config{}, old)
	last := probe.lastLog
	if last.IsZero() {
		t.Fatal("no log timestamp parsed")
	}

	idle := openAt(t, dir, Config{}, last.Add(StaleAfter+time.Second)).Snapshot().Pool
	if !idle.Stale || idle.StaleReason != "log idle" || !idle.LastActivity.Equal(last.UTC()) {
		t.Fatalf("idle log not stale: %+v", idle)
	}
	if idle.Hashrate1m <= 0 || idle.Users <= 0 {
		t.Fatalf("raw values must be kept: %+v", idle)
	}
	if !idle.AllAliveSince.IsZero() {
		t.Fatalf("all-alive streak set on a stale pool: %v", idle.AllAliveSince)
	}

	if p := openAt(t, dir, Config{}, last.Add(StaleAfter-time.Second)).Snapshot().Pool; p.Stale || p.StaleReason != "" {
		t.Fatalf("recent log marked stale: %+v", p)
	}
	if p := openAt(t, dir, Config{}, last.Add(-time.Hour)).Snapshot().Pool; p.Stale {
		t.Fatalf("clock behind the log marked stale: %+v", p)
	}
}

func TestStaleUsesNewerMtime(t *testing.T) {
	tr, dir := tree(t, false)
	probe := openAt(t, dir, Config{}, fixturegen.FixtureStart)
	now := probe.lastLog.Add(10 * StaleAfter)
	if err := os.Chtimes(tr.LogPath, now.Add(-time.Second), now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if p := openAt(t, dir, Config{}, now).Snapshot().Pool; p.Stale {
		t.Fatalf("fresh mtime ignored: %+v", p)
	}
}

const procTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0D05 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 11111 1 0 100 0 0 10 0
   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 22222 1 0 100 0 0 10 0
   2: 0100007F:0D06 0100007F:C350 01 00000000:00000000 00:00000000 00000000     0        0 33333 1 0 100 0 0 10 0
`

const procTCP6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:1A0B 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 44444 1 0 100 0 0 10 0
   1: 00000000000000000000000001000000:0D07 00000000000000000000000000000000:0000 06 00000000:00000000 00:00000000 00000000     0        0 55555 1 0 100 0 0 10 0
`

func TestParseListen(t *testing.T) {
	got := parseListen(procTCP)
	if !got[3333] || !got[8080] || len(got) != 2 {
		t.Fatalf("ipv4 listeners = %v", got)
	}
	if got[3334] {
		t.Fatal("ESTABLISHED socket counted as listening")
	}
	got6 := parseListen(procTCP6)
	if !got6[6667] || got6[3335] || len(got6) != 1 {
		t.Fatalf("ipv6 listeners = %v", got6)
	}
	if len(parseListen("")) != 0 || len(parseListen("garbage\nx y\n")) != 0 {
		t.Fatal("malformed content must yield nothing")
	}
}

func TestStratumPorts(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "conf")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, c := range []struct {
		body string
		want []int
	}{
		{`{"serverurl": ["192.0.2.1:3333", "192.0.2.1:3334", "192.0.2.2:3333"]}`, []int{3333, 3334}},
		{`{"serverurl": "192.0.2.1:4444"}`, []int{4444}},
		{`{"serverurl": ["192.0.2.1"]}`, []int{3333}},
		{`{"serverurl": ["[2001:db8::1]:5555"]}`, []int{5555}},
		{`{"btcd": []}`, nil},
		{`not json`, nil},
	} {
		got := stratumPorts(write(c.body))
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: got %v want %v", c.body, got, c.want)
		}
	}
	if stratumPorts("") != nil || stratumPorts(filepath.Join(dir, "missing")) != nil {
		t.Fatal("unreadable conf must yield no ports")
	}
}

func TestStaleWithoutListener(t *testing.T) {
	tr, dir := tree(t, false)
	conf := filepath.Join(t.TempDir(), "conf")
	if err := os.WriteFile(conf, []byte(`{"serverurl": ["192.0.2.1:3333"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	proc := t.TempDir()
	put := func(name, body string) {
		if err := os.WriteFile(filepath.Join(proc, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := fixturegen.FixtureStart
	_ = os.Chtimes(tr.LogPath, old, old)
	last := openAt(t, dir, Config{}, old).lastLog
	fresh := last.Add(time.Second)
	cfg := Config{Conf: conf, ProcNet: proc}

	put("tcp", procTCP)
	if p := openAt(t, dir, cfg, fresh).Snapshot().Pool; p.Stale {
		t.Fatalf("listener present but stale: %+v", p)
	}
	put("tcp", "  sl  local_address\n")
	if p := openAt(t, dir, cfg, fresh).Snapshot().Pool; !p.Stale || p.StaleReason != "no listener on :3333" {
		t.Fatalf("missing listener not flagged: %+v", p)
	}
	if p := openAt(t, dir, cfg, last.Add(time.Hour)).Snapshot().Pool; p.StaleReason != "log idle; no listener on :3333" {
		t.Fatalf("both reasons expected: %+v", p)
	}
	if err := os.Remove(filepath.Join(proc, "tcp")); err != nil {
		t.Fatal(err)
	}
	if p := openAt(t, dir, cfg, fresh).Snapshot().Pool; p.Stale {
		t.Fatalf("unreadable proc must skip the listener signal: %+v", p)
	}
}

type rewardNode struct {
	fail  map[int64]bool
	calls map[int64]int
}

func (n *rewardNode) BlockHash(_ context.Context, h int64) (string, error) {
	n.calls[h]++
	if n.fail[h] {
		return "", errors.New("response larger than 1 MB")
	}
	return fmt.Sprintf("hash%d", h), nil
}

func (n *rewardNode) BlockReward(context.Context, string) (rpc.Reward, error) {
	return rpc.Reward{BCH: 3.125, Satoshis: 312500000}, nil
}

func (n *rewardNode) IsOrphaned(context.Context, int64, string) (bool, error) { return false, nil }

func TestEnrichMissingBacksOffFailingHeights(t *testing.T) {
	tr, dir := tree(t, true)
	_ = tr
	now := fixturegen.FixtureStart
	var logs bytes.Buffer
	a, err := Open(Config{Root: dir, Memory: true, NoNode: true,
		Now: func() time.Time { return now }, Log: log.New(&logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	var heights []int64
	for _, b := range a.ix.Snapshot().Blocks {
		if b.Status == index.StatusAccepted {
			heights = append(heights, b.Height)
		}
	}
	if len(heights) < 3 {
		t.Skipf("fixture has %d accepted blocks", len(heights))
	}
	n := &rewardNode{fail: map[int64]bool{heights[0]: true, heights[1]: true}, calls: map[int64]int{}}
	a.enricher = n

	for i := 0; i < len(heights); i++ {
		a.enrichMissing(context.Background(), 2)
	}
	for _, h := range heights[2:] {
		if _, ok := a.rewards[h]; !ok {
			t.Fatalf("height %d never enriched", h)
		}
	}
	if n.calls[heights[0]] != 1 || n.calls[heights[1]] != 1 {
		t.Fatalf("failing heights retried inside backoff: %v", n.calls)
	}
	if got := strings.Count(logs.String(), "reward lookup failed"); got != 2 {
		t.Fatalf("logged %d failures, want 2:\n%s", got, logs.String())
	}

	now = now.Add(2 * time.Minute)
	a.enrichMissing(context.Background(), 8)
	if n.calls[heights[0]] != 2 || n.calls[heights[1]] != 2 {
		t.Fatalf("no retry after backoff: %v", n.calls)
	}
	a.enrichMissing(context.Background(), 8)
	if n.calls[heights[0]] != 2 {
		t.Fatalf("second backoff step not applied: %v", n.calls)
	}
	now = now.Add(2 * time.Minute)
	a.enrichMissing(context.Background(), 8)
	if n.calls[heights[0]] != 2 {
		t.Fatalf("backoff did not grow: %v", n.calls)
	}
	now = now.Add(4 * time.Minute)
	a.enrichMissing(context.Background(), 8)
	if n.calls[heights[0]] != 3 {
		t.Fatalf("no retry after second step: %v", n.calls)
	}
}

func bestLine(addr, rig string, diff float64, at time.Time) string {
	return fmt.Sprintf(`{"workinfoid":1,"diff":1,"sdiff":%g,"result":true,"createdate":"%d,0","workername":"%s.%s","username":"%s","address":"%s"}`+"\n",
		diff, at.Unix(), addr, rig, addr, fixturegen.IP(9))
}

// ownBest keeps the leaderboard rows for the workers a test wrote itself; the
// fixture's own sharelogs also have accepted shares after its last block.
func ownBest(rows []ui.BestShare, addr string, rigs ...string) []ui.BestShare {
	var out []ui.BestShare
	for _, r := range rows {
		for _, rig := range rigs {
			if r.Worker == addr+"."+rig {
				out = append(out, r)
			}
		}
	}
	return out
}

// The leaderboard only holds shares after the pool's last block and never
// reaches the network difficulty. A sharelog share above the pool's own
// round best is kept: the daemon only raises that number from shares that
// also beat the submitting connection's per-user-round best, so it lags the
// sharelogs for a whole round after a block found by someone else.
func TestBestSharesSinceLastBlock(t *testing.T) {
	_, dir := tree(t, true)
	a := open(t, dir)
	s0 := a.Snapshot()
	last, pb := s0.Pool.LastBlock, s0.Pool.BestShare
	if last.IsZero() || pb <= 0 {
		t.Fatalf("fixture lacks a block or a round best: %v %v", last, pb)
	}
	addr := fixturegen.BareAddr(3)
	nd := filepath.Join(dir, "logs", "7fffffff")
	if err := os.MkdirAll(nd, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := bestLine(addr, "old", pb*0.9, last.Add(-time.Hour)) +
		bestLine(addr, "big", pb*2, last.Add(time.Second)) +
		bestLine(addr, "rig1", pb/4, last.Add(time.Second)) +
		bestLine(addr, "rig2", pb/2, last.Add(2*time.Second))
	if err := os.WriteFile(filepath.Join(nd, "0000000000000001.sharelog"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.ix.Sync(); err != nil {
		t.Fatal(err)
	}
	s := a.Snapshot()
	if own := ownBest(s.BestShares, addr, "old", "big", "rig1", "rig2"); len(own) != 3 || own[0].Worker != addr+".big" || own[1].Worker != addr+".rig2" || own[2].Worker != addr+".rig1" {
		t.Fatalf("best shares %+v", s.BestShares)
	}
	for _, b := range s.BestShares {
		if !b.Time.After(s.Pool.LastBlock) {
			t.Fatalf("%+v not after the last block", b)
		}
	}
	if s.BestShares[0].Diff > s.Pool.BestShare || s.BestShares[0].Diff >= s.Pool.NetworkDiff {
		t.Fatalf("top %v above pool best %v or network %v", s.BestShares[0].Diff, s.Pool.BestShare, s.Pool.NetworkDiff)
	}
}

type deadNode struct{ calls int }

func (n *deadNode) BlockHash(context.Context, int64) (string, error) {
	n.calls++
	return "", errors.New("node down")
}

func (n *deadNode) BlockReward(context.Context, string) (rpc.Reward, error) {
	n.calls++
	return rpc.Reward{}, errors.New("node down")
}

func (n *deadNode) IsOrphaned(context.Context, int64, string) (bool, error) {
	n.calls++
	return false, errors.New("node down")
}

func openCached(t *testing.T, dir string) *App {
	t.Helper()
	a, err := Open(Config{Root: dir, NoNode: true, Now: func() time.Time {
		return fixturegen.FixtureStart.Add(41 * time.Minute)
	}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Rewards and the round's best shares come back from the cache on restart,
// with no node and no rescan; a new block empties the round for good.
func TestRestartServesRewardsAndRoundFromCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	tr, dir := tree(t, true)

	a := openCached(t, dir)
	last, pb := a.Snapshot().Pool.LastBlock, a.Snapshot().Pool.BestShare
	if last.IsZero() || pb <= 0 {
		a.Close()
		t.Fatalf("fixture lacks a block or round best: %v %v", last, pb)
	}
	addr := fixturegen.BareAddr(3)
	nd := filepath.Join(dir, "logs", "7fffffff")
	if err := os.MkdirAll(nd, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := bestLine(addr, "rig1", pb/4, last.Add(time.Second)) + bestLine(addr, "rig2", pb/2, last.Add(2*time.Second))
	if err := os.WriteFile(filepath.Join(nd, "0000000000000001.sharelog"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	a.enricher = &rewardNode{calls: map[int64]int{}}
	a.enrichMissing(context.Background(), 100)
	if err := a.ix.Sync(); err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for _, b := range a.Snapshot().Blocks {
		if b.Status != ui.BlockRejected && b.Status != ui.BlockOrphaned {
			accepted++
		}
	}
	if accepted == 0 {
		t.Fatal("no accepted blocks")
	}
	a.Close()

	dead := &deadNode{}
	b := openCached(t, dir)
	b.enricher = dead
	b.enrichMissing(context.Background(), 100)
	s := b.Snapshot()
	for _, r := range s.Blocks {
		if r.Status != ui.BlockRejected && r.Status != ui.BlockOrphaned && r.Reward != 312500000 {
			t.Fatalf("block %d reward %d after restart", r.Height, r.Reward)
		}
	}
	if dead.calls != 0 {
		t.Fatalf("refetched %d times", dead.calls)
	}
	if own := ownBest(s.BestShares, addr, "rig1", "rig2"); len(own) != 2 || own[0].Worker != addr+".rig2" {
		t.Fatalf("round best after restart: %+v", s.BestShares)
	}

	g := fixturegen.New(21, fixturegen.FixtureStart.Add(24*time.Hour))
	h := int(s.Blocks[0].Height) + 10
	appendLog(t, tr.LogPath, g.BlockCluster(h, fixturegen.FullWorker(1, 0), "SV1"))
	if err := b.ix.Sync(); err != nil {
		t.Fatal(err)
	}
	if s := b.Snapshot(); len(s.BestShares) != 0 {
		t.Fatalf("round survived a new block: %+v", s.BestShares)
	}
	b.Close()

	c := openCached(t, dir)
	defer c.Close()
	if s := c.Snapshot(); len(s.BestShares) != 0 || len(s.Blocks) == 0 {
		t.Fatalf("stale entries after second restart: %+v", s.BestShares)
	}
}

func TestLiveShareNewerThanPoolStatusKeptInLeaderboard(t *testing.T) {
	_, dir := tree(t, true)
	a := open(t, dir)
	s0 := a.Snapshot()
	last, pb := s0.Pool.LastBlock, s0.Pool.BestShare
	if last.IsZero() || pb <= 0 {
		t.Fatalf("fixture lacks a block or round best: %v %v", last, pb)
	}
	a.mu.Lock()
	poolUpd := time.Unix(a.stats.pool.LastUpdate, 0).UTC()
	a.mu.Unlock()
	if poolUpd.IsZero() {
		t.Fatal("pool.status lacks lastupdate")
	}

	addr := fixturegen.BareAddr(6)
	nd := filepath.Join(dir, "logs", "7fffffff")
	if err := os.MkdirAll(nd, 0o755); err != nil {
		t.Fatal(err)
	}
	// A share newer than pool.status lastupdate that exceeds stale poolBest (but is < netDiff)
	// must be kept in BestShares and promote Pool.BestShare.
	newBest := pb * 1.5
	if s0.Pool.NetworkDiff > 0 && newBest >= s0.Pool.NetworkDiff {
		newBest = s0.Pool.NetworkDiff * 0.5
	}
	shareAt := poolUpd.Add(5 * time.Second)
	if !shareAt.After(last) {
		shareAt = last.Add(5 * time.Second)
	}
	if err := os.WriteFile(filepath.Join(nd, "0000000000000001.sharelog"), []byte(bestLine(addr, "livebest", newBest, shareAt)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.ix.Sync(); err != nil {
		t.Fatal(err)
	}
	s := a.Snapshot()
	own := ownBest(s.BestShares, addr, "livebest")
	if len(own) != 1 || own[0].Diff != newBest {
		t.Fatalf("newer-than-pool.status share dropped: %+v (all=%+v)", own, s.BestShares)
	}
	if s.Pool.BestShare < newBest {
		t.Fatalf("Pool.BestShare %v did not advance to live share %v", s.Pool.BestShare, newBest)
	}
}
