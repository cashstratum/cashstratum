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
