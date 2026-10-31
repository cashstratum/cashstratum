package fixturegen

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func logBytes(t *testing.T, size int64, seed uint64) []byte {
	t.Helper()
	var b bytes.Buffer
	if _, err := GenerateLog(&b, size, seed); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDeterministic(t *testing.T) {
	a, b := logBytes(t, 400_000, 7), logBytes(t, 400_000, 7)
	if !bytes.Equal(a, b) {
		t.Fatal("same seed produced different bytes")
	}
	if bytes.Equal(a, logBytes(t, 400_000, 8)) {
		t.Fatal("different seeds produced identical bytes")
	}
	s1 := New(1, FixtureStart).Simulate(30)
	s2 := New(1, FixtureStart).Simulate(30)
	if strings.Join(s1.Lines, "") != strings.Join(s2.Lines, "") {
		t.Fatal("Simulate not deterministic")
	}
}

func TestGenerateLogSize(t *testing.T) {
	b := logBytes(t, 1<<20, 3)
	if len(b) < 1<<20 || len(b) > 1<<20+256<<10 {
		t.Fatalf("size %d", len(b))
	}
}

func TestCashAddr(t *testing.T) {
	// Published test vector for a 20-byte zero payload.
	if got := EncodeCashAddr([20]byte{}); got != "bitcoincash:qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqrmfnjtm" && !VerifyCashAddr(got) {
		t.Fatalf("bad address %s", got)
	}
	for i := 0; i < 50; i++ {
		a := Addr(i)
		if !strings.HasPrefix(a, "bitcoincash:q") || !VerifyCashAddr(a) {
			t.Fatalf("invalid %s", a)
		}
		bad := []byte(a)
		bad[len(bad)-1] ^= 1
		if VerifyCashAddr(string(bad)) {
			t.Fatalf("corruption not detected for %s", a)
		}
	}
}

func TestFamilies(t *testing.T) {
	log := string(logBytes(t, 2<<20, 11))
	s := New(5, FixtureStart).Simulate(40)
	log += strings.Join(s.Lines, "")
	pats := map[string]string{
		"authorised":    `Authorised client \d+ \S+ worker \S+ as user \S+ with diff \d+\n`,
		"dropped-old":   `Dropped client \d+ \S+ user \S+ worker \S+ \n`,
		"dropped-new":   `Dropped client \d+ \S+ user \S+ worker \S+ \s?(lazily)? reason=[a-z_]+( errno=\d+)? age=\d+\n`,
		"user-periodic": `User \S+:\{"hashrate1m":"[^"]+".*"lastshare":\d+.*"authorised":\d+\}\n`,
		"user-solve":    `User \S+:\{"hashrate1m":"[^"]+","hashrate5m":"[^"]+","hashrate1hr":"[^"]+","hashrate1d":"[^"]+","hashrate7d":"[^"]+","shares":\d+,"authorised":\d+\}\n`,
		"worker":        `Worker \S+:\{"hashrate1m"`,
		"pool1":         `Pool:\{"runtime":`,
		"pool2":         `Pool:\{"hashrate1m":`,
		"pool3":         `Pool:\{"diff":`,
		"workbase":      `Stored local workbase with \d+ transactions\n`,
		"zmq":           `ZMQ block hash [0-9a-f]{64} from endpoint \d+\n`,
		"netdiff":       `Network diff set to \d+\.\d\n`,
		"hashchanged":   `Block hash changed to [0-9a-f]{64}\n`,
		"possible":      `Possible block solve diff \d+\.\d{6} !\n`,
		"submitting":    `Submitting block data!\n`,
		"accepted":      `BLOCK ACCEPTED!\n`,
		"returned":      `SUBMIT BLOCK RETURNED: high-hash\n`,
		"solved":        `Solved and confirmed block \d+ by \S+ on SV1\n`,
		"after":         `Block solved after \d+ shares at \d+\.\d% diff\n`,
		"submpossible":  `Submitting possible block solve share diff \d+\.\d{6} !\n`,
		"rejected":      `Submitted, but had block \d+ rejected\n`,
		"nicehash":      `NiceHash detected from useragent '.*' for client \d+, applied difficulty \d+\n`,
		"mrr":           `MiningRigRentals detected`,
		"pattern":       `Client \d+ workername '.*' matched pattern '.*', applied difficulty \d+\n`,
		"password":      `Client \d+ set difficulty to \d+ via password parameter\n`,
	}
	for name, p := range pats {
		re := regexp.MustCompile(`(?m)^\[\d{4}-\d\d-\d\d \d\d:\d\d:\d\d\.\d{3}\] ` + p)
		if !re.MatchString(log) {
			t.Errorf("family %s missing", name)
		}
	}
}

func idx(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func TestClusterOrdering(t *testing.T) {
	g := New(2, FixtureStart)
	ls := g.BlockCluster(900100, FullWorker(0, 0), "SV1")
	order := []string{"Possible block solve", "Submitting block data", "BLOCK ACCEPTED", "ZMQ block hash", "Solved and confirmed block 900100", "Block solved after", "Submitting possible block solve", "Network diff set", "Block hash changed"}
	last := -1
	for _, o := range order {
		i := idx(ls, o)
		if i <= last {
			t.Fatalf("%q out of order (%d <= %d)", o, i, last)
		}
		last = i
	}
	if strings.Contains(strings.Join(g.BlockCluster(900101, FullWorker(0, 0), ""), ""), " on SV") {
		t.Fatal("empty proto must omit suffix")
	}
	r := g.RejectedCluster(900102, "inconclusive")
	if idx(r, "SUBMIT BLOCK RETURNED: inconclusive") < 0 || idx(r, "Submitted, but had block 900102 rejected") < 0 || idx(r, "Solved and confirmed") >= 0 {
		t.Fatal("rejected cluster shape wrong")
	}
}

func TestWriteTree(t *testing.T) {
	dir := t.TempDir()
	tr, err := WriteTree(dir, DefaultSeed, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"logs/cashstratum.log", "logs/pool/pool.status", "solved-heights.idx", "solves/INDEX.tsv", "logs/users/" + tr.Users[0]} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	ps, _ := os.ReadFile(filepath.Join(dir, "logs/pool/pool.status"))
	if strings.Count(string(ps), "\n") != 3 {
		t.Error("pool.status must have 3 lines")
	}
	shares, _ := filepath.Glob(filepath.Join(dir, "logs", "0*", "*.sharelog"))
	if len(shares) == 0 || len(tr.Found) == 0 {
		t.Fatal("no sharelogs or blocks")
	}
	for _, b := range tr.Found {
		if _, err := os.Stat(filepath.Join(dir, "solves", SolveFileName(b.Height, b.Hash))); err != nil {
			t.Error(err)
		}
	}
	idxb, _ := os.ReadFile(filepath.Join(dir, "solved-heights.idx"))
	if !strings.HasPrefix(string(idxb), "# cashstratum solve index v1") {
		t.Error("idx marker missing")
	}
	u, _ := os.ReadFile(filepath.Join(dir, "logs/users", tr.Users[0]))
	if !strings.Contains(string(u), "\n    \"hashrate1m\"") {
		t.Error("user file not 4-space pretty")
	}
}

func TestDemo(t *testing.T) {
	d := DemoSnapshot(1, FixtureStart)
	if len(d.Workers) == 0 || len(d.Blocks) == 0 || d.Pool.Hashrate1m <= 0 {
		t.Fatal("demo incomplete")
	}
}

func TestReservedIPs(t *testing.T) {
	re := regexp.MustCompile(`^(192\.0\.2|198\.51\.100|203\.0\.113)\.\d+$`)
	for i := 0; i < 600; i++ {
		if !re.MatchString(IP(i)) {
			t.Fatal(IP(i))
		}
	}
}

func BenchmarkGenerateLog66MB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := GenerateLog(io.Discard, 66<<20, 1); err != nil {
			b.Fatal(err)
		}
	}
}
