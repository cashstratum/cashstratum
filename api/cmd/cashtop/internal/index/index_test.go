package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"cashstratumapi/cmd/cashtop/internal/tail"
	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/poolfmt"
)

type env struct {
	t    *testing.T
	root string
	log  string
	tree *fixturegen.Tree
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	root := t.TempDir()
	tree, err := fixturegen.WriteTree(root, 7, true)
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, root: root, log: tree.LogPath, tree: tree, now: time.Unix(1_000_000, 0)}
}

func (e *env) open(mut ...func(*Options)) *Index {
	e.t.Helper()
	o := Options{Root: e.root, Tail: tail.Options{Now: func() time.Time { return e.now }}}
	for _, m := range mut {
		m(&o)
	}
	ix, err := Open(o)
	if err != nil {
		e.t.Fatal(err)
	}
	return ix
}

func (e *env) cacheDir() string { return CacheDirFor(e.root) }

func appendFile(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func countIn(t *testing.T, p, needle string) int {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), needle)
}

func sessions(v View) int {
	n := 0
	for _, w := range v.Workers {
		n += w.Sessions
	}
	return n
}

func solvedBlocks(v View) int {
	n := 0
	for _, b := range v.Blocks {
		if b.Status == StatusAccepted {
			n++
		}
	}
	return n
}

// extra builds log text with n Authorised lines and, when blocks is true,
// three block clusters at new heights; it returns the text, the Authorised
// count and the heights.
func extra(seed uint64, auth int, heights ...int) (string, int, []int) {
	g := fixturegen.New(seed, fixturegen.FixtureStart.Add(24*time.Hour))
	var sb strings.Builder
	for i := 0; i < auth; i++ {
		sb.WriteString(g.Authorised())
		g.Advance(time.Millisecond)
	}
	for _, h := range heights {
		for _, l := range g.BlockCluster(h, fixturegen.FullWorker(1, 0), "SV1") {
			sb.WriteString(l)
		}
	}
	return sb.String(), auth, heights
}

func TestRebuildMatchesLog(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	info := ix.Info()
	if !info.Rescanned || !info.Persistent {
		t.Fatalf("info %+v", info)
	}
	v := ix.Snapshot()
	if want := countIn(t, e.log, "Authorised client "); sessions(v) != want {
		t.Fatalf("sessions %d want %d", sessions(v), want)
	}
	if got, want := solvedBlocks(v), countIn(t, e.log, "Solved and confirmed block "); got != want {
		t.Fatalf("accepted %d want %d", got, want)
	}
	byH := map[int64]Block{}
	for _, b := range v.Blocks {
		byH[b.Height] = b
	}
	for _, f := range e.tree.Found {
		b := byH[int64(f.Height)]
		if b.Hash != f.Hash || b.Finder != f.Finder || b.Luck == nil || b.Status != StatusAccepted {
			t.Fatalf("block %d: %+v want %+v", f.Height, b, f)
		}
	}
	found := map[int]bool{}
	for _, f := range e.tree.Found {
		found[f.Height] = true
	}
	for _, h := range e.tree.Rejected {
		if found[h] {
			continue // a later accepted submit at the same height wins
		}
		if b := byH[int64(h)]; b.Status != StatusRejected || b.Reason == "" {
			t.Fatalf("rejected %d: %+v", h, b)
		}
	}
	if len(v.Hashrate) == 0 || len(v.PoolLuck) == 0 || len(v.Best) == 0 {
		t.Fatalf("series empty: hr=%d luck=%d best=%d", len(v.Hashrate), len(v.PoolLuck), len(v.Best))
	}
	for i := 1; i < len(v.Best); i++ {
		if v.Best[i].Diff > v.Best[i-1].Diff {
			t.Fatal("best not sorted")
		}
	}
	for _, w := range v.Workers {
		if w.Address == "" || strings.Contains(w.Address, ".") {
			t.Fatalf("worker address %q", w.Address)
		}
	}
}

func TestWorkSumsMatchSharelogs(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	v := ix.Snapshot()
	h, dir, ok := ix.newestHeightDir()
	if !ok || v.Work.Height != h {
		t.Fatalf("height %d want %d", v.Work.Height, h)
	}
	want := map[string]float64{}
	files, _ := filepath.Glob(filepath.Join(dir, "*.sharelog"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		for _, l := range strings.Split(string(b), "\n") {
			if s, err := poolfmt.ParseSharelogLine(l); err == nil && s.Result {
				want[PayoutAddress(s)] += s.Diff
			}
		}
	}
	if len(want) == 0 || !reflect.DeepEqual(want, v.Work.Sums) {
		t.Fatalf("sums %v want %v", v.Work.Sums, want)
	}

	// New lines in the same file add to the sums; a new height starts over.
	appendFile(t, files[0], fixturegen.New(1, fixturegen.FixtureStart).ShareLine(1, true, 100, 100)+"\n")
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	var before, after float64
	for _, x := range want {
		before += x
	}
	for _, x := range ix.Snapshot().Work.Sums {
		after += x
	}
	if after != before+100 {
		t.Fatalf("incremental: %v -> %v", before, after)
	}
	nd := filepath.Join(e.root, "logs", fmt.Sprintf("%08x", h+1))
	os.MkdirAll(nd, 0o755)
	appendFile(t, filepath.Join(nd, "0000000000000001.sharelog"), fixturegen.New(2, fixturegen.FixtureStart).ShareLine(1, true, 7, 7)+"\n")
	ix.Sync()
	w := ix.Snapshot().Work
	var tot float64
	for _, x := range w.Sums {
		tot += x
	}
	if w.Height != h+1 || tot != 7 {
		t.Fatalf("after new height: %+v", w)
	}
}

// The sharelog's "address" is the miner's IP: work belongs to the username.
func TestWorkKeyedByPayoutNotIP(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	h, _, ok := ix.newestHeightDir()
	if !ok {
		t.Fatal("no height dir")
	}
	nd := filepath.Join(e.root, "logs", fmt.Sprintf("%08x", h+1))
	if err := os.MkdirAll(nd, 0o755); err != nil {
		t.Fatal(err)
	}
	payout, ip := fixturegen.Addr(2), fixturegen.IP(5)
	lines := fmt.Sprintf(`{"workinfoid":1,"diff":300,"sdiff":400,"result":true,"workername":"%[1]s.rig01","username":"%[2]s","address":"%[3]s"}
{"workinfoid":1,"diff":200,"sdiff":250,"result":true,"workername":"%[1]s.rig02","username":"","address":"%[3]s"}
{"workinfoid":1,"diff":999,"sdiff":1,"result":false,"workername":"%[1]s.rig01","username":"%[2]s","address":"%[3]s"}
`, fixturegen.BareAddr(2), payout, ip)
	appendFile(t, filepath.Join(nd, "0000000000000001.sharelog"), lines)
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	w := ix.Snapshot().Work
	want := map[string]float64{fixturegen.BareAddr(2): 500}
	if w.Height != h+1 || !reflect.DeepEqual(w.Sums, want) {
		t.Fatalf("sums %v at %d, want %v", w.Sums, w.Height, want)
	}
	if _, ok := w.Sums[ip]; ok {
		t.Fatal("the IP was credited")
	}
}

func TestRestartResumesWithoutRescan(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	first := ix.Snapshot()

	more, auth, _ := extra(11, 5, 9001)
	appendFile(t, e.log, more)

	ix2 := e.open()
	info := ix2.Info()
	if info.Rescanned || info.Lines > int64(strings.Count(more, "\n")) {
		t.Fatalf("info %+v", info)
	}
	v := ix2.Snapshot()
	if sessions(v) != sessions(first)+auth || solvedBlocks(v) != solvedBlocks(first)+1 {
		t.Fatalf("sessions %d/%d blocks %d/%d", sessions(v), sessions(first), solvedBlocks(v), solvedBlocks(first))
	}
	ix2.Close()

	ix3 := e.open()
	defer ix3.Close()
	if ix3.Info().Rescanned || ix3.Info().Lines != 0 {
		t.Fatalf("info %+v", ix3.Info())
	}
	got, want := ix3.Snapshot(), v
	got.Cursor, want.Cursor = tail.Cursor{}, tail.Cursor{}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("state differs after restart")
	}
}

func TestSecondInstanceIsInMemory(t *testing.T) {
	e := newEnv(t)
	a := e.open()
	defer a.Close()
	b := e.open()
	defer b.Close()
	if b.Info().Persistent || !a.Info().Persistent {
		t.Fatalf("a=%+v b=%+v", a.Info(), b.Info())
	}
	if sessions(b.Snapshot()) != sessions(a.Snapshot()) {
		t.Fatal("in-memory scan differs")
	}
}

func TestRenameRotationLosesAndDuplicatesNothing(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	base := ix.Snapshot()
	var wantAuth int
	var wantBlocks int
	feed := func(path string, auth int, heights ...int) {
		s, a, h := extra(uint64(100+wantAuth), auth, heights...)
		appendFile(t, path, s)
		wantAuth += a
		wantBlocks += len(h)
	}
	feed(e.log, 4, 9001)
	ix.Sync()
	feed(e.log, 3, 9002)
	if err := os.Rename(e.log, e.log+".1"); err != nil {
		t.Fatal(err)
	}
	feed(e.log, 6, 9003)
	ix.Sync()
	// The daemon keeps writing the old inode until it reopens its log.
	feed(e.log+".1", 2, 9004)
	feed(e.log, 5, 9005)
	e.now = e.now.Add(30 * time.Second)
	ix.Sync()
	e.now = e.now.Add(40 * time.Second)
	ix.Sync()
	ix.Sync()
	v := ix.Snapshot()
	if got := sessions(v) - sessions(base); got != wantAuth {
		t.Fatalf("authorised lines: got %d want %d", got, wantAuth)
	}
	if got := solvedBlocks(v) - solvedBlocks(base); got != wantBlocks {
		t.Fatalf("solved events: got %d want %d", got, wantBlocks)
	}
}

func TestCopytruncateLosesAndDuplicatesNothing(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	base := ix.Snapshot()
	s1, a1, _ := extra(21, 4, 9001)
	appendFile(t, e.log, s1)
	ix.Sync()
	if err := os.Truncate(e.log, 0); err != nil {
		t.Fatal(err)
	}
	s2, a2, _ := extra(22, 3, 9002, 9003)
	appendFile(t, e.log, s2)
	ix.Sync()
	ix.Sync()
	v := ix.Snapshot()
	if got := sessions(v) - sessions(base); got != a1+a2 {
		t.Fatalf("authorised: got %d want %d", got, a1+a2)
	}
	if got := solvedBlocks(v) - solvedBlocks(base); got != 3 {
		t.Fatalf("solved: got %d want 3", got)
	}
}

func TestTornLineHeldUntilNewline(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	n := sessions(ix.Snapshot())
	line := fixturegen.New(5, fixturegen.FixtureStart).Authorised()
	appendFile(t, e.log, line[:len(line)-10])
	ix.Sync()
	if sessions(ix.Snapshot()) != n {
		t.Fatal("torn line counted")
	}
	appendFile(t, e.log, line[len(line)-10:])
	ix.Sync()
	if sessions(ix.Snapshot()) != n+1 {
		t.Fatal("completed line not counted exactly once")
	}
}

func warm(t *testing.T, e *env, lines string) *Index {
	t.Helper()
	ix := e.open()
	ix.Close()
	appendFile(t, e.log, lines)
	ix = e.open()
	if ix.Info().Rescanned {
		t.Fatalf("setup rescanned: %+v", ix.Info())
	}
	ix.Close()
	return ix
}

func TestCorruptJournalRescans(t *testing.T) {
	e := newEnv(t)
	want := sessions(func() View { ix := e.open(); defer ix.Close(); return ix.Snapshot() }())
	more, auth, _ := extra(31, 4, 9001)
	warm(t, e, more)
	jp := filepath.Join(e.cacheDir(), journalName)
	raw, _ := os.ReadFile(jp)
	if len(raw) < 100 {
		t.Fatalf("journal too small: %d", len(raw))
	}
	lines := strings.SplitN(string(raw), "\n", 3)
	lines[1] = "{not json"
	os.WriteFile(jp, []byte(strings.Join(lines, "\n")), 0o600)

	ix := e.open()
	defer ix.Close()
	if !ix.Info().Rescanned || ix.Info().Reason == "" {
		t.Fatalf("info %+v", ix.Info())
	}
	if sessions(ix.Snapshot()) != want+auth {
		t.Fatalf("sessions %d want %d", sessions(ix.Snapshot()), want+auth)
	}
}

func TestCorruptSnapshotRescans(t *testing.T) {
	e := newEnv(t)
	e.open().Close()
	os.WriteFile(filepath.Join(e.cacheDir(), snapshotName), []byte("garbage"), 0o600)
	ix := e.open()
	defer ix.Close()
	if !ix.Info().Rescanned || sessions(ix.Snapshot()) == 0 {
		t.Fatalf("info %+v", ix.Info())
	}
}

func TestTornJournalTailIsIgnored(t *testing.T) {
	e := newEnv(t)
	more, auth, _ := extra(32, 3)
	warm(t, e, more)
	want := sessions(func() View { ix := e.open(); defer ix.Close(); return ix.Snapshot() }())
	appendFile(t, filepath.Join(e.cacheDir(), journalName), `{"k":"worker","worker":{"name":"x`)
	ix := e.open()
	defer ix.Close()
	if ix.Info().Rescanned {
		t.Fatalf("rescanned: %+v", ix.Info())
	}
	if sessions(ix.Snapshot()) != want || want == 0 || auth == 0 {
		t.Fatal("state changed")
	}
}

func TestVersionBumpRescans(t *testing.T) {
	for _, field := range []string{"schema", "parser"} {
		t.Run(field, func(t *testing.T) {
			e := newEnv(t)
			e.open().Close()
			sp := filepath.Join(e.cacheDir(), snapshotName)
			raw, _ := os.ReadFile(sp)
			var m map[string]json.RawMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			m[field] = json.RawMessage("999")
			raw, _ = json.Marshal(m)
			os.WriteFile(sp, raw, 0o600)
			ix := e.open()
			defer ix.Close()
			if !ix.Info().Rescanned || !strings.Contains(ix.Info().Reason, "version") {
				t.Fatalf("info %+v", ix.Info())
			}
		})
	}
}

func TestStaleTmpFilesAreIgnored(t *testing.T) {
	e := newEnv(t)
	e.open().Close()
	dir := e.cacheDir()
	os.WriteFile(filepath.Join(dir, snapshotName+".tmp"), []byte(`{"half":`), 0o600)
	os.WriteFile(filepath.Join(dir, journalName+".tmp"), []byte("xx"), 0o600)
	ix := e.open()
	defer ix.Close()
	if ix.Info().Rescanned {
		t.Fatalf("rescanned: %+v", ix.Info())
	}
	if _, err := os.Stat(filepath.Join(dir, snapshotName+".tmp")); !os.IsNotExist(err) {
		t.Fatal("stale tmp left behind")
	}
}

func TestStaleJournalGenerationIgnored(t *testing.T) {
	// A crash after the new snapshot was renamed in but before the new
	// journal replaced the old one leaves an older-generation journal.
	e := newEnv(t)
	more, _, _ := extra(33, 3, 9001)
	warm(t, e, more)
	want := sessions(func() View { ix := e.open(); defer ix.Close(); return ix.Snapshot() }())
	ix := e.open()
	ix.mu.Lock()
	ix.gen += 5
	err := ix.compact()
	ix.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	ix.Close()
	hdr, _ := json.Marshal(rec{K: "hdr", Schema: SchemaVersion, Parser: ParserVersion, Gen: 1})
	junk := string(hdr) + "\n" + `{"k":"worker","worker":{"name":"ghost.rig","sessions":99}}` + "\n" + `{"k":"cur","cur":{}}` + "\n"
	os.WriteFile(filepath.Join(e.cacheDir(), journalName), []byte(junk), 0o600)
	ix2 := e.open()
	defer ix2.Close()
	v := ix2.Snapshot()
	if ix2.Info().Rescanned || sessions(v) != want {
		t.Fatalf("info %+v sessions %d want %d", ix2.Info(), sessions(v), want)
	}
}

func TestLogReplacedRescans(t *testing.T) {
	e := newEnv(t)
	e.open().Close()
	os.Remove(e.log)
	s, auth, _ := extra(41, 6, 9001)
	appendFile(t, e.log, s)
	ix := e.open()
	defer ix.Close()
	v := ix.Snapshot()
	if !ix.Info().Rescanned || sessions(v) != auth || solvedBlocks(v) < 1 {
		t.Fatalf("info %+v sessions %d", ix.Info(), sessions(v))
	}
}

func TestPrunedHeightDirIsHarmless(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	h, dir, _ := ix.newestHeightDir()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	h2, _, ok := ix.newestHeightDir()
	if ok && h2 >= h {
		t.Fatalf("newest still %d", h2)
	}
	if ok {
		if w := ix.Snapshot().Work; w.Height != h2 {
			t.Fatalf("work height %d want %d", w.Height, h2)
		}
	}
	os.RemoveAll(filepath.Join(e.root, "logs"))
	os.MkdirAll(filepath.Dir(e.log), 0o755)
	appendFile(t, e.log, "")
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestSolvedHeightsReplacedViaRename(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	if _, ok := func() (Block, bool) {
		for _, b := range ix.Snapshot().Blocks {
			if b.Height == 77777 {
				return b, true
			}
		}
		return Block{}, false
	}(); ok {
		t.Fatal("unexpected block")
	}
	hs := []int{77777}
	for _, f := range e.tree.Found {
		hs = append(hs, f.Height)
	}
	tmp := filepath.Join(e.root, "solved-heights.idx.new")
	os.WriteFile(tmp, fixturegen.SolvedHeightsIdx(hs), 0o644)
	if err := os.Rename(tmp, filepath.Join(e.root, "solved-heights.idx")); err != nil {
		t.Fatal(err)
	}
	ix.Sync()
	var got *Block
	v := ix.Snapshot()
	for i := range v.Blocks {
		if v.Blocks[i].Height == 77777 {
			got = &v.Blocks[i]
		}
	}
	if got == nil || got.Status != StatusAccepted {
		t.Fatalf("block from replaced idx missing: %+v", got)
	}
	uniq := map[int]bool{77777: true}
	for _, f := range e.tree.Found {
		uniq[f.Height] = true
	}
	for _, h := range e.tree.Rejected {
		uniq[h] = true
	}
	if len(v.Blocks) != len(uniq) {
		t.Fatalf("blocks %d", len(v.Blocks))
	}
}

func TestOrphanVerdictSurvivesRestartAndRescan(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	h := int64(e.tree.Found[0].Height)
	ix.SetBlockStatus(h, StatusOrphaned)
	ix.Close()
	status := func(ix *Index) string {
		for _, b := range ix.Snapshot().Blocks {
			if b.Height == h {
				return b.Status
			}
		}
		return ""
	}
	ix2 := e.open()
	if status(ix2) != StatusOrphaned {
		t.Fatalf("after restart: %s", status(ix2))
	}
	ix2.Close()
	ix3 := e.open(func(o *Options) { o.ForceRescan = true })
	defer ix3.Close()
	if status(ix3) != StatusOrphaned {
		t.Fatalf("after rescan: %s", status(ix3))
	}
}

func TestApplyLiveEvent(t *testing.T) {
	e := newEnv(t)
	ix := e.open(func(o *Options) { o.Memory = true })
	defer ix.Close()
	n := sessions(ix.Snapshot())
	ev := poolfmt.ParseLine(fixturegen.New(9, fixturegen.FixtureStart).Authorised())
	ix.Apply(ev, 123)
	if sessions(ix.Snapshot()) != n+1 {
		t.Fatal("event not applied")
	}
}

func TestWarmStartOn66MBLog(t *testing.T) {
	if testing.Short() {
		t.Skip("66 MB benchmark")
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	root := t.TempDir()
	lp := filepath.Join(root, "logs", "cashstratum.log")
	os.MkdirAll(filepath.Dir(lp), 0o755)
	f, err := os.Create(lp)
	if err != nil {
		t.Fatal(err)
	}
	n, err := fixturegen.GenerateLog(f, 66<<20, 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	ix, err := Open(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	first := time.Since(t0)
	lines := ix.Info().Lines
	ix.Close()
	t0 = time.Now()
	ix2, err := Open(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	second := time.Since(t0)
	defer ix2.Close()
	t.Logf("log %d bytes, %d lines: first scan %v, warm start %v", n, lines, first, second)
	if ix2.Info().Rescanned || ix2.Info().Lines != 0 {
		t.Fatalf("warm start rescanned: %+v", ix2.Info())
	}
	if second > 2*time.Second {
		t.Fatalf("warm start took %v", second)
	}
}

func TestSplitLogGivesSameView(t *testing.T) {
	build := func(split bool) View {
		t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
		root := t.TempDir()
		if _, err := fixturegen.WriteTreeOpts(root, 7, true, fixturegen.TreeOpts{Verbose: true, Split: split}); err != nil {
			t.Fatal(err)
		}
		ix, err := Open(Options{Root: root, Memory: true})
		if err != nil {
			t.Fatal(err)
		}
		defer ix.Close()
		return ix.Snapshot()
	}
	plain, split := build(false), build(true)
	plain.Cursor, split.Cursor = tail.Cursor{}, tail.Cursor{}
	if len(plain.Blocks) == 0 || len(plain.Blocks) != len(split.Blocks) {
		t.Fatalf("blocks: %d vs %d", len(plain.Blocks), len(split.Blocks))
	}
	// Byte offsets legitimately differ between the two layouts.
	for i := range plain.Blocks {
		plain.Blocks[i].Offset, split.Blocks[i].Offset = 0, 0
	}
	if !reflect.DeepEqual(plain, split) {
		t.Fatalf("views differ:\n%+v\n%+v", plain, split)
	}
}

func roundLine(addr, rig string, diff float64, at time.Time, ok bool) string {
	return fmt.Sprintf(`{"workinfoid":1,"diff":1,"sdiff":%g,"result":%t,"createdate":"%d,5","workername":"%s.%s","username":"%s","address":"%s"}`+"\n",
		diff, ok, at.Unix(), addr, rig, addr, fixturegen.IP(9))
}

func roundKeys(v View) []string {
	var out []string
	for _, b := range v.RoundBest {
		out = append(out, fmt.Sprintf("%s=%g", b.Address, b.Diff))
	}
	return out
}

// ownRoundKeys is roundKeys limited to the workers a test wrote itself; the
// fixture's own sharelogs also have accepted shares after its last block.
func ownRoundKeys(v View, workers ...string) []string {
	var out []string
	for _, b := range v.RoundBest {
		for _, w := range workers {
			if b.Address == w {
				out = append(out, fmt.Sprintf("%s=%g", b.Address, b.Diff))
			}
		}
	}
	return out
}

// A huge share from before the last block never shows; the shares after it
// rank by difficulty per worker, and a new block starts the round over.
func TestRoundBestSinceLastBlock(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	v := ix.Snapshot()
	if len(v.Blocks) == 0 {
		t.Fatal("no blocks")
	}
	_, at, ok := ix.lastAccepted()
	if !ok || at.IsZero() {
		t.Fatal("no timed accepted block")
	}
	h := v.Blocks[0].Height
	a, b := fixturegen.BareAddr(3), fixturegen.BareAddr(4)
	nd := filepath.Join(e.root, "logs", fmt.Sprintf("%08x", h+1))
	if err := os.MkdirAll(nd, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(nd, "0000000000000001.sharelog")
	appendFile(t, f, roundLine(a, "rig1", 9e12, at.Add(-time.Hour), true)+
		roundLine(a, "rig1", 500, at.Add(time.Second), true)+
		roundLine(b, "rig2", 300, at.Add(2*time.Second), true)+
		roundLine(a, "rig1", 200, at.Add(3*time.Second), true)+
		roundLine(b, "rig2", 1e9, at.Add(4*time.Second), false)+
		roundLine(a, "rig3", 100, at.Add(-time.Second), true))
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	v = ix.Snapshot()
	own := []string{a + ".rig1", a + ".rig3", b + ".rig2"}
	want := []string{a + ".rig1=500", b + ".rig2=300"}
	if got := ownRoundKeys(v, own...); !v.RoundSeen || !reflect.DeepEqual(got, want) {
		t.Fatalf("round best %v, want %v", got, want)
	}
	for _, x := range v.RoundBest {
		if !x.Time.After(at) {
			t.Fatalf("share %v is not after the block at %v", x, at)
		}
	}

	// Appended shares fold in without a rescan.
	appendFile(t, f, roundLine(b, "rig2", 700, at.Add(5*time.Second), true))
	ix.Sync()
	if got := ownRoundKeys(ix.Snapshot(), own...); !reflect.DeepEqual(got, []string{b + ".rig2=700", a + ".rig1=500"}) {
		t.Fatalf("after append: %v", got)
	}

	// A new block resets the round; only newer shares count.
	text, _, hs := extra(21, 0, int(h)+10)
	appendFile(t, e.log, text)
	ix.Sync()
	v = ix.Snapshot()
	if len(v.RoundBest) != 0 {
		t.Fatalf("round survived the block: %v", roundKeys(v))
	}
	_, at2, _ := ix.lastAccepted()
	if !at2.After(at) || v.Blocks[0].Height != int64(hs[0]) {
		t.Fatalf("new block not applied: %v %v", at2, v.Blocks[0].Height)
	}
	nd2 := filepath.Join(e.root, "logs", fmt.Sprintf("%08x", hs[0]+1))
	os.MkdirAll(nd2, 0o755)
	appendFile(t, filepath.Join(nd2, "0000000000000001.sharelog"), roundLine(a, "rig1", 42, at2.Add(time.Second), true))
	ix.Sync()
	if got := ownRoundKeys(ix.Snapshot(), own...); !reflect.DeepEqual(got, []string{a + ".rig1=42"}) {
		t.Fatalf("new round: %v", got)
	}

	// The round survives a restart.
	ix.Close()
	ix2 := e.open()
	defer ix2.Close()
	if got := ownRoundKeys(ix2.Snapshot(), own...); !reflect.DeepEqual(got, []string{a + ".rig1=42"}) {
		t.Fatalf("after restart: %v", got)
	}
}

// Without sharelogs the User lines feed the table, but only those logged
// after the last block.
func TestFallbackBestOnlyAfterLastBlock(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	_, at, _ := ix.lastAccepted()
	for _, b := range ix.Snapshot().Best {
		if !b.Time.After(at) {
			t.Fatalf("fallback entry %v is not after the block at %v", b, at)
		}
	}
}

func TestBlockRewardPersists(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	h := ix.Snapshot().Blocks[0].Height
	ix.SetBlockReward(h, 312500000)
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	ix2 := e.open()
	defer ix2.Close()
	if r := ix2.Snapshot().Blocks[0].Reward; r != 312500000 {
		t.Fatalf("reward after restart: %d", r)
	}
}

// Enrichment is durable the moment it is set: a copy of the cache taken
// before any Close or Sync already carries it.
func TestEnrichmentDurableWithoutClose(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	h := ix.Snapshot().Blocks[0].Height
	ix.SetBlockReward(h, 312500000)
	ix.SetBlockHash(h, "nodehash")
	ix.SetTip(h + 3)

	dir := t.TempDir()
	for _, n := range []string{snapshotName, journalName} {
		raw, err := os.ReadFile(filepath.Join(ix.dir, n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, n), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ix2 := e.open(func(o *Options) { o.CacheDir = dir })
	defer ix2.Close()
	if ix2.Info().Rescanned {
		t.Fatal("copy was rescanned")
	}
	v := ix2.Snapshot()
	if b := v.Blocks[0]; b.Reward != 312500000 || b.Hash != "nodehash" || v.Tip != h+3 {
		t.Fatalf("lost after restart: %+v tip %d", b, v.Tip)
	}
}

// A rescan keeps the reward and node hash of a block whose solve is the same.
func TestRescanKeepsEnrichment(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	h := ix.Snapshot().Blocks[0].Height
	ix.SetBlockReward(h, 312500000)
	ix.SetBlockHash(h, "nodehash")
	ix.SetTip(h + 3)
	ix.Close()
	ix2 := e.open(func(o *Options) { o.ForceRescan = true })
	defer ix2.Close()
	v := ix2.Snapshot()
	if b := v.Blocks[0]; b.Height != h || b.Reward != 312500000 || b.Hash != "nodehash" || v.Tip != h+3 {
		t.Fatalf("rescan dropped enrichment: %+v tip %d", b, v.Tip)
	}
}

func TestSolveContextPopulatesLuckAndProto(t *testing.T) {
	e := newEnv(t)
	solvesDir := filepath.Join(e.root, "solves")
	if err := os.MkdirAll(solvesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	h := int64(888888)
	hash := "0000000000000000000123456789abcdef0123456789abcdef0123456789abcd"
	ctxFile := fmt.Sprintf("solves/%d-%s.log", h, hash)
	ctxContent := fmt.Sprintf(
		"[2026-04-19 10:00:00.100] Solved and confirmed block %d by bitcoincash:qp0000000000000000000000000000000000000000.rig1 on SV2\n"+
			"[2026-04-19 10:00:00.101] Block solved after 123456 shares at 142.5%% diff\n",
		h,
	)
	if err := os.WriteFile(filepath.Join(e.root, ctxFile), []byte(ctxContent), 0o644); err != nil {
		t.Fatal(err)
	}
	tsvLine := fmt.Sprintf("%d\t%s\t2026-04-19 10:00:00.100\tACCEPTED\tbitcoincash:qp0000000000000000000000000000000000000000.rig1\t10\t%s\n", h, hash, ctxFile)
	appendFile(t, filepath.Join(solvesDir, "INDEX.tsv"), tsvLine)

	ix := e.open()
	defer ix.Close()
	var got *Block
	for i := range ix.Snapshot().Blocks {
		b := &ix.Snapshot().Blocks[i]
		if b.Height == h {
			got = b
			break
		}
	}
	if got == nil || got.Luck == nil || *got.Luck != 142.5 || got.Proto != "SV2" {
		t.Fatalf("solve context not merged: %+v", got)
	}
}

func TestSyncRoundSkipsSolvedBlockOwnDirectory(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	defer ix.Close()
	h, at, ok := ix.lastAccepted()
	if !ok || at.IsZero() {
		t.Fatal("no timed accepted block")
	}
	a := fixturegen.BareAddr(5)
	solvedDir := filepath.Join(e.root, "logs", fmt.Sprintf("%08x", h))
	if err := os.MkdirAll(solvedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Share written to the solved block's own directory 1 second after block solve time must be ignored.
	appendFile(t, filepath.Join(solvedDir, "0000000000000099.sharelog"), roundLine(a, "oldround", 999999, at.Add(time.Second), true))
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := ownRoundKeys(ix.Snapshot(), a+".oldround"); len(got) != 0 {
		t.Fatalf("solved block's own sharelog directory leaked into new round: %v", got)
	}
}

func TestWorkerAgentAndDiffPersist(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	v := ix.Snapshot()
	var name string
	for _, w := range v.Workers {
		if w.Agent != "" && w.LastDiff > 0 {
			name = w.Name
			break
		}
	}
	if name == "" {
		t.Fatal("no worker picked up an agent and difficulty from the sharelogs")
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	ix2 := e.open()
	defer ix2.Close()
	if ix2.Info().Rescanned {
		t.Fatalf("info %+v", ix2.Info())
	}
	for _, w := range ix2.Snapshot().Workers {
		if w.Name == name && w.Agent != "" && w.LastDiff > 0 {
			return
		}
	}
	t.Fatalf("%s lost its agent or diff over a restart", name)
}

// A snapshot written before Agent and LastDiff existed still loads, without a
// rescan, and leaves the new fields empty.
func TestSnapshotWithoutAgentFieldsLoads(t *testing.T) {
	e := newEnv(t)
	e.open().Close()
	p := filepath.Join(e.cacheDir(), snapshotName)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	old := regexp.MustCompile(`,"(agent|last_diff)":("[^"]*"|[0-9.eE+-]+)`).ReplaceAll(raw, nil)
	if len(old) == len(raw) {
		t.Fatal("snapshot held no agent fields to strip")
	}
	if err := os.WriteFile(p, old, 0o600); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(e.cacheDir(), journalName))
	ix := e.open()
	defer ix.Close()
	if ix.Info().Rescanned {
		t.Fatalf("old snapshot forced a rescan: %+v", ix.Info())
	}
	v := ix.Snapshot()
	if len(v.Workers) == 0 {
		t.Fatal("no workers loaded")
	}
	for _, w := range v.Workers {
		if w.Agent != "" || w.LastDiff != 0 {
			t.Fatalf("%s: agent %q diff %v from an old snapshot", w.Name, w.Agent, w.LastDiff)
		}
	}
}

const (
	addrNice  = "qqnwep7p0802q2e4mpkeze7afer9zj3l8qryyg3chh"
	addrBare  = "qrfc9tvc5dx4ywgmnxj5yy95xdrg7ra9eupjeshhfq"
	addrDeus  = "qpdap8727zduldlgyk7x94yxq49cu4l7lc29tat3a3"
	addrLegcy = "14BDgNEHdr6eo8wEQSfu8uAcV6HgG2GzxH"
)

// userLuck is a periodic User line, which carries the user's own round.
func userLuck(stamp, user string, luck float64) string {
	return fmt.Sprintf(`[%s] User %s:{"hashrate1m":"4.5T","hashrate5m":"4.4T","hashrate1hr":"4.3T","hashrate1d":"4T","hashrate7d":"4T","lastshare":1762480786,"workers":1,"shares":175954,"roundshares":1966086000000,"bestshare":12345678,"luck":%v,"networkdiff":552305174002.9905,"bestever":1,"authorised":1700000000}`+"\n", stamp, user, luck)
}

// userSolve is the User line printed with a solve: no roundshares, no luck.
func userSolve(stamp, user string) string {
	return fmt.Sprintf(`[%s] User %s:{"hashrate1m":"4.5T","hashrate5m":"4.4T","hashrate1hr":"4.3T","hashrate1d":"4T","hashrate7d":"4T","shares":175954,"authorised":1700000000}`+"\n", stamp, user)
}

// bareIndex opens an in-memory index over a pool root whose log is text.
func bareIndex(t *testing.T, text string) (*Index, string) {
	t.Helper()
	root := t.TempDir()
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "cashstratum.log"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, err := Open(Options{Root: root, Memory: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix, root
}

func blockAt(v View, h int64) *Block {
	for i := range v.Blocks {
		if v.Blocks[i].Height == h {
			return &v.Blocks[i]
		}
	}
	return nil
}

func workerNamed(v View, name string) *Worker {
	for i := range v.Workers {
		if v.Workers[i].Name == name {
			return &v.Workers[i]
		}
	}
	return nil
}

func stampOf(t *testing.T, line string) time.Time {
	t.Helper()
	ev := poolfmt.ParseLine(line)
	if !ev.HasTime {
		t.Fatalf("no time in %q", line)
	}
	return ev.Time
}

// The finder's effort is the luck of its last periodic User line before the
// solve; the luck-less User line printed with the solve is ignored, and the
// pool effort still comes from the "Block solved after" line.
func TestFinderLuckFromLastUserLine(t *testing.T) {
	user := "bitcoincash:" + addrNice
	log := userLuck("2026-11-07 01:59:46.947", user, 355.978196935985) +
		userSolve("2026-11-07 01:59:50.000", user) +
		"[2026-11-07 02:00:00.646] Solved and confirmed block 976298 by " + user + ".nice on SV1\n" +
		userSolve("2026-11-07 02:00:00.646", user) +
		"[2026-11-07 02:00:00.646] Block solved after 65606500000 shares at 11.9% diff\n"
	ix, _ := bareIndex(t, log)
	b := blockAt(ix.Snapshot(), 976298)
	if b == nil || b.FinderLuck == nil || b.Luck == nil {
		t.Fatalf("block %+v", b)
	}
	if *b.FinderLuck != 355.978196935985 || *b.Luck != 11.9 {
		t.Fatalf("finder %v pool %v, want 355.978196935985 and 11.9", *b.FinderLuck, *b.Luck)
	}
}

// Solve lines name the finder bare, prefixed or in legacy form; User lines
// always carry the prefix for cash addresses. All of them match.
func TestFinderLuckMatchesEveryAddressForm(t *testing.T) {
	log := userLuck("2026-11-01 10:00:00.000", "bitcoincash:"+addrBare, 50.42) +
		userLuck("2026-11-01 10:00:01.000", addrLegcy, 127.98) +
		userLuck("2026-11-01 10:00:02.000", "bitcoincash:"+addrNice, 427.98) +
		"[2026-11-01 10:00:30.000] Solved and confirmed block 975568 by " + addrBare + ".doutor1 on SV1\n" +
		"[2026-11-01 10:00:40.000] Solved and confirmed block 975569 by " + addrLegcy + ".rig on SV1\n" +
		"[2026-11-01 10:00:50.000] Solved and confirmed block 975570 by bitcoincash:" + addrNice + " on SV1\n"
	ix, _ := bareIndex(t, log)
	v := ix.Snapshot()
	for h, want := range map[int64]float64{975568: 50.42, 975569: 127.98, 975570: 427.98} {
		b := blockAt(v, h)
		if b == nil || b.FinderLuck == nil || *b.FinderLuck != want {
			t.Fatalf("%d: %+v, want %v", h, b, want)
		}
	}
}

// The finder's own round restarts at its solve: a second block before its
// next periodic line has no known effort, and one after it reads the small
// fresh value. Another address's block leaves the finder's value alone.
func TestFinderLuckBackToBackSameAddress(t *testing.T) {
	user := "bitcoincash:" + addrNice
	other := "bitcoincash:" + addrBare
	log := userLuck("2026-10-26 10:00:00.000", user, 120.5) +
		userLuck("2026-10-26 10:00:01.000", other, 80.25) +
		"[2026-10-26 10:00:10.000] Solved and confirmed block 975757 by " + user + ".nice on SV1\n" +
		userSolve("2026-10-26 10:00:10.000", user) +
		"[2026-10-26 10:00:40.000] Solved and confirmed block 975758 by " + user + ".nice on SV1\n" +
		userLuck("2026-10-26 10:01:00.000", user, 0.34) +
		"[2026-10-26 10:01:20.000] Solved and confirmed block 975759 by " + user + ".nice on SV1\n" +
		"[2026-10-26 10:01:30.000] Solved and confirmed block 975760 by " + other + ".rig on SV1\n"
	ix, _ := bareIndex(t, log)
	v := ix.Snapshot()
	if b := blockAt(v, 975757); b == nil || b.FinderLuck == nil || *b.FinderLuck != 120.5 {
		t.Fatalf("first: %+v", b)
	}
	if b := blockAt(v, 975758); b == nil || b.FinderLuck != nil {
		t.Fatalf("second solve before a new User line must be unknown: %+v", b)
	}
	if b := blockAt(v, 975759); b == nil || b.FinderLuck == nil || *b.FinderLuck != 0.34 {
		t.Fatalf("third: %+v", b)
	}
	if b := blockAt(v, 975760); b == nil || b.FinderLuck == nil || *b.FinderLuck != 80.25 {
		t.Fatalf("other address: %+v", b)
	}
	if _, ok := v.LastLuck[addrNice]; ok {
		t.Fatalf("consumed luck still remembered: %v", v.LastLuck)
	}
}

// The remembered luck outlives a cashtop restart, so a solve tailed after
// the restart still gets the finder's effort.
func TestFinderLuckSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	ix := e.open()
	user := "bitcoincash:" + addrNice
	appendFile(t, e.log, userLuck("2026-11-07 01:59:46.947", user, 355.978196935985))
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	ix2 := e.open()
	defer ix2.Close()
	if ix2.Info().Rescanned {
		t.Fatalf("unexpected rescan: %+v", ix2.Info())
	}
	appendFile(t, e.log, "[2026-11-07 02:00:00.646] Solved and confirmed block 976298 by "+user+".nice on SV1\n")
	if err := ix2.Sync(); err != nil {
		t.Fatal(err)
	}
	if b := blockAt(ix2.Snapshot(), 976298); b == nil || b.FinderLuck == nil || *b.FinderLuck != 355.978196935985 {
		t.Fatalf("block %+v", b)
	}
}

// A block missing from the log is backfilled from its solves/ context file,
// finder effort included.
func TestSolveContextPopulatesFinderLuck(t *testing.T) {
	e := newEnv(t)
	solvesDir := filepath.Join(e.root, "solves")
	if err := os.MkdirAll(solvesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	h := int64(888889)
	hash := "0000000000000000000123456789abcdef0123456789abcdef0123456789abce"
	ctxFile := fmt.Sprintf("solves/%d-%s.log", h, hash)
	ctx := userLuck("2026-04-19 09:59:40.000", "bitcoincash:"+addrBare, 61.5) +
		fmt.Sprintf("[2026-04-19 10:00:00.100] Solved and confirmed block %d by %s.doutor1 on SV1\n", h, addrBare) +
		userSolve("2026-04-19 10:00:00.100", "bitcoincash:"+addrBare) +
		"[2026-04-19 10:00:00.101] Block solved after 123456 shares at 142.5% diff\n"
	if err := os.WriteFile(filepath.Join(e.root, ctxFile), []byte(ctx), 0o644); err != nil {
		t.Fatal(err)
	}
	appendFile(t, filepath.Join(solvesDir, "INDEX.tsv"),
		fmt.Sprintf("%d\t%s\t2026-04-19 10:00:00.100\tACCEPTED\t%s.doutor1\t10\t%s\n", h, hash, addrBare, ctxFile))
	ix := e.open()
	defer ix.Close()
	b := blockAt(ix.Snapshot(), h)
	if b == nil || b.FinderLuck == nil || *b.FinderLuck != 61.5 || b.Luck == nil || *b.Luck != 142.5 {
		t.Fatalf("solve context not merged: %+v", b)
	}
}

// A pool restart logs no drop lines, yet ends every session: a worker
// authorised before it is not online after it.
func TestPoolRestartClosesSessions(t *testing.T) {
	user := "bitcoincash:" + addrDeus
	worker := user + ".deus"
	auth := "[2026-11-06 09:28:59.000] Authorised client 3258 34.22.147.138 worker " + worker + " as user " + user + " with diff 10000\n"
	restart := "[2026-11-06 11:51:44.961] cashstratum stratifier starting\n"
	log := auth + auth + restart + "[2026-11-06 11:51:45.100] cashstratum stratifier ready\n"
	ix, _ := bareIndex(t, log)
	w := workerNamed(ix.Snapshot(), worker)
	if w == nil {
		t.Fatal("worker missing")
	}
	at := stampOf(t, restart)
	if w.Online || w.Clients != 0 || !w.Disconnected.Equal(at) || w.DropReason != ReasonPoolRestart {
		t.Fatalf("after restart: %+v", w)
	}
	if want := int64(at.Sub(stampOf(t, auth)) / time.Second); w.Longest != want {
		t.Fatalf("longest %d, want %d", w.Longest, want)
	}
	if w.Streak(at.Add(time.Hour)) != 0 {
		t.Fatal("closed session still has a streak")
	}

	reauth := "[2026-11-06 11:52:10.000] Authorised client 2 34.22.147.138 worker " + worker + " as user " + user + " with diff 10000\n"
	ix.Apply(poolfmt.ParseLine(reauth), 1)
	w = workerNamed(ix.Snapshot(), worker)
	if !w.Online || w.Clients != 1 || !w.Disconnected.IsZero() || w.DropReason != "" || !w.Since.Equal(stampOf(t, reauth)) {
		t.Fatalf("after reconnect: %+v", w)
	}
}

// A drop to zero sessions records when and why; an older drop line without
// a reason records the time only; a reconnect clears both. The pool API's
// own probe never becomes a worker.
func TestDropRecordsReasonAndReauthClears(t *testing.T) {
	user := "bitcoincash:" + addrDeus
	worker := user + ".NerdaxeGaia"
	auth := func(stamp, id string) string {
		return "[" + stamp + "] Authorised client " + id + " 176.206.42.215 worker " + worker + " as user " + user + " with diff 10000\n"
	}
	drop1 := "[2026-11-08 14:54:34.482] Dropped client 389 176.206.42.215 user " + user + " worker " + worker + "  reason=peer_reset errno=104 age=153\n"
	drop2 := "[2026-11-08 15:34:09.049] Dropped client 472 176.206.42.215 user " + user + " worker " + worker + "  reason=peer_eof age=2360\n"
	probe := "bitcoincash:qr5pp33g5ul4l8chfg56au2kpvanfmqryqfgv00utu"
	log := auth("2026-11-08 14:52:00.000", "389") + auth("2026-11-08 14:54:00.000", "472") +
		drop1 +
		"[2026-11-08 15:00:00.000] Authorised client 900 127.0.0.1 worker " + probe + ".cashstratum-api as user " + probe + " with diff 1\n" +
		"[2026-11-08 15:00:01.000] Dropped client 900 127.0.0.1 user " + probe + " worker " + probe + ".cashstratum-api  reason=peer_eof age=1\n"
	ix, _ := bareIndex(t, log)
	v := ix.Snapshot()
	w := workerNamed(v, worker)
	if w == nil || !w.Online || w.Clients != 1 || !w.Disconnected.IsZero() {
		t.Fatalf("one of two sessions dropped must stay online: %+v", w)
	}
	if p := workerNamed(v, probe+".cashstratum-api"); p != nil {
		t.Fatalf("API probe indexed as a worker: %+v", p)
	}

	ix.Apply(poolfmt.ParseLine(drop2), 1)
	w = workerNamed(ix.Snapshot(), worker)
	if w.Online || !w.Disconnected.Equal(stampOf(t, drop2)) || w.DropReason != "peer_eof" {
		t.Fatalf("after last drop: %+v", w)
	}

	ix.Apply(poolfmt.ParseLine(auth("2026-11-08 15:40:00.000", "500")), 2)
	if w = workerNamed(ix.Snapshot(), worker); !w.Online || !w.Disconnected.IsZero() || w.DropReason != "" {
		t.Fatalf("after reconnect: %+v", w)
	}

	old := "[2026-11-08 15:50:00.000] Dropped client 500 176.206.42.215 user " + user + " worker " + worker + " \n"
	ix.Apply(poolfmt.ParseLine(old), 3)
	if w = workerNamed(ix.Snapshot(), worker); w.Online || !w.Disconnected.Equal(stampOf(t, old)) || w.DropReason != "" {
		t.Fatalf("after a drop without reason: %+v", w)
	}
}

func sumLine(addr, rig string, diff, sdiff float64, at time.Time, ok bool) string {
	return fmt.Sprintf(`{"workinfoid":1,"diff":%g,"sdiff":%g,"result":%t,"createdate":"%d,5","workername":"bitcoincash:%s.%s","username":"bitcoincash:%s","address":"%s"}`+"\n",
		diff, sdiff, ok, at.Unix(), addr, rig, addr, fixturegen.IP(9))
}

// The round sums each payout address's accepted assigned difficulty since
// the pool's last block, block-sized shares included, and a shrunk sharelog
// rebuilds the round instead of counting its shares twice.
func TestRoundSumsAcceptedDiff(t *testing.T) {
	solve := "[2026-11-08 08:10:50.124] Solved and confirmed block 976456 by bitcoincash:" + addrNice + ".nice on SV1\n"
	ix, root := bareIndex(t, solve+userLuck("2026-11-08 08:11:00.000", "bitcoincash:"+addrNice, 0.5))
	at := stampOf(t, solve)
	dir := filepath.Join(root, "logs", fmt.Sprintf("%08x", 976457))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "0000000000000001.sharelog")
	body := sumLine(addrNice, "nice", 10000, 50000, at.Add(-time.Second), true) + // before the block
		sumLine(addrNice, "nice", 10000, 20000, at.Add(time.Second), true) +
		sumLine(addrNice, "nice", 10000, 9e12, at.Add(2*time.Second), true) + // block-sized
		sumLine(addrBare, "doutor1", 4000, 8000, at.Add(3*time.Second), true) +
		sumLine(addrBare, "doutor1", 4000, 1, at.Add(4*time.Second), false) // rejected
	appendFile(t, f, body)
	if err := ix.Sync(); err != nil {
		t.Fatal(err)
	}
	v := ix.Snapshot()
	want := map[string]float64{addrNice: 20000, addrBare: 4000}
	if !reflect.DeepEqual(v.RoundSums, want) {
		t.Fatalf("sums %v, want %v", v.RoundSums, want)
	}
	if !v.RoundSince.Equal(at) || v.RoundHeight != 976456 {
		t.Fatalf("round since %v height %d", v.RoundSince, v.RoundHeight)
	}
	if v.RoundAddrBest[addrNice] != 20000 || v.RoundAddrBest[addrBare] != 8000 {
		t.Fatalf("per-address best %v", v.RoundAddrBest)
	}

	// Appended shares fold in incrementally.
	appendFile(t, f, sumLine(addrBare, "doutor1", 4000, 8000, at.Add(5*time.Second), true))
	ix.Sync()
	if got := ix.Snapshot().RoundSums[addrBare]; got != 8000 {
		t.Fatalf("after append %v", got)
	}

	// A truncated and rewritten sharelog rebuilds the round from scratch.
	if err := os.WriteFile(f, []byte(sumLine(addrBare, "doutor1", 4000, 8000, at.Add(6*time.Second), true)), 0o644); err != nil {
		t.Fatal(err)
	}
	ix.Sync()
	if got := ix.Snapshot().RoundSums; !reflect.DeepEqual(got, map[string]float64{addrBare: 4000}) {
		t.Fatalf("after shrink %v", got)
	}
}

// A cache written by the previous parser is rebuilt from the log, which
// fills fields the old parser never derived.
func TestPreviousParserVersionRescans(t *testing.T) {
	e := newEnv(t)
	user := "bitcoincash:" + addrNice
	appendFile(t, e.log, userLuck("2026-11-07 01:59:46.947", user, 355.978196935985)+
		"[2026-11-07 02:00:00.646] Solved and confirmed block 976298 by "+user+".nice on SV1\n")
	e.open().Close()
	sp := filepath.Join(e.cacheDir(), snapshotName)
	raw, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	var sn snapshot
	if err := json.Unmarshal(raw, &sn); err != nil {
		t.Fatal(err)
	}
	sn.Parser = ParserVersion - 1
	sn.State.Blocks[976298].FinderLuck = nil
	raw, _ = json.Marshal(sn)
	if err := os.WriteFile(sp, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ix := e.open()
	defer ix.Close()
	if !ix.Info().Rescanned || !strings.Contains(ix.Info().Reason, "version") {
		t.Fatalf("info %+v", ix.Info())
	}
	if b := blockAt(ix.Snapshot(), 976298); b == nil || b.FinderLuck == nil {
		t.Fatalf("rescan did not backfill: %+v", b)
	}
}
