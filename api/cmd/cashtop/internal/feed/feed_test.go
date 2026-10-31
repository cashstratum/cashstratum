package feed

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	addrA = "qzexampleaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	addrB = "qzexamplebbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func runLines(e *Engine, lines ...string) []string {
	var out []string
	for i, l := range lines {
		for _, r := range e.Feed(l, int64(i)) {
			out = append(out, r.Text)
		}
	}
	for _, r := range e.Flush() {
		out = append(out, r.Text)
	}
	return out
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func authLine(t, cid, ip, w, addr string) string {
	return "[2026-01-01 " + t + ".000] Authorised client " + cid + " " + ip + " worker " + w + " as user bitcoincash:" + addr + " with diff 1024"
}

func dropLine(t, cid, ip, w, addr, extra string) string {
	return "[2026-01-01 " + t + ".000] Dropped client " + cid + " " + ip + " user bitcoincash:" + addr + " worker " + w + extra
}

func TestIdleUserHiddenRejectsKept(t *testing.T) {
	e := NewEngine(Config{})
	mk := func(h, extra string) string {
		return `[2026-01-01 00:00:02.000] User bitcoincash:` + addrA + `:{"hashrate1m":"` + h + `","hashrate5m":"2P","hashrate1hr":"3P","workers":2,"bestshare":1978744860.9,"luck":1.4174988` + extra + `}`
	}
	if got := runLines(e, mk("0", "")); len(got) != 0 {
		t.Fatalf("idle shown: %v", got)
	}
	if got := runLines(e, mk("0", `,"rejectedcount":0`)); len(got) != 0 {
		t.Fatalf("zero rejects shown: %v", got)
	}
	got := runLines(e, mk("0", `,"rejectedcount":7`))
	if len(got) != 1 || !strings.Contains(got[0], "rej 7") {
		t.Fatalf("rejects row: %v", got)
	}
	got = runLines(e, mk("1.4P", ""))
	if len(got) != 1 || !strings.Contains(got[0], "2w  best 1.98G  luck 1.42%") {
		t.Fatalf("active row: %v", got)
	}
}

func TestAPIProbeHidesAuthorisedAndDropped(t *testing.T) {
	e := NewEngine(Config{})
	got := runLines(e,
		authLine("00:00:03", "1", "127.0.0.1", addrA+".cashstratum-api", addrA),
		dropLine("00:00:04", "1", "127.0.0.1", addrA+".cashstratum-api", addrA, ""))
	if len(got) != 0 {
		t.Fatalf("probe visible: %v", got)
	}
	// same worker name from another address is a real miner
	got = runLines(e, authLine("00:00:05", "2", "192.0.2.1", addrA+".cashstratum-api", addrA))
	if len(got) != 1 {
		t.Fatalf("real miner hidden: %v", got)
	}
}

func TestDifficultyTagsConsumedOnce(t *testing.T) {
	e := NewEngine(Config{})
	got := runLines(e,
		"[2026-01-01 00:00:05.000] NiceHash detected from useragent 'NiceHash/1.0.0' for client 2, applied difficulty 500000",
		"[2026-01-01 00:00:05.000] Client 2 set difficulty to 10000000 via password parameter",
		authLine("00:00:05", "2", "192.0.2.10", addrA+".rig1", addrA),
		authLine("00:00:06", "2", "192.0.2.10", addrA+".rig1", addrA))
	want0 := "00:00:05 + " + addrA + ".rig1  192.0.2.10  d=1024  [NiceHash floor 500000 d=10000000 (password)]"
	if len(got) != 2 || got[0] != want0 || strings.Contains(got[1], "[") {
		t.Fatalf("got %q", got)
	}
}

func TestTagMapCapped(t *testing.T) {
	e := NewEngine(Config{MaxTracked: 8})
	for i := 0; i < 100; i++ {
		e.Feed("[2026-01-01 00:00:05.000] Client "+strings.Repeat("9", 1)+string(rune('a'+i%26))+string(rune('a'+i/26))+" workername x matched pattern y applied difficulty 5", 0)
	}
	if n := len(e.tags.m); n > 8 {
		t.Fatalf("tag map grew to %d", n)
	}
}

func flapRows(gap, sess, n int) []string {
	var lines []string
	t := 100
	for i := 0; i < n; i++ {
		cid := string(rune('1' + i))
		lines = append(lines,
			authLine(hms(t), cid, "192.0.2.20", addrB+".rig9", addrB),
			dropLine(hms(t+sess), cid, "192.0.2.20", addrB+".rig9", addrB, ""))
		t += sess + gap
	}
	return runLines(NewEngine(Config{}), lines...)
}

func hms(s int) string {
	d := func(v int) string { return string([]byte{byte('0' + v/10), byte('0' + v%10)}) }
	return d(s/3600) + ":" + d(s/60%60) + ":" + d(s%60)
}

func TestFlapFoldsAfterFirstShortDrop(t *testing.T) {
	out := flapRows(1, 1, 3)
	var flaps, plus3 int
	for _, r := range out {
		if strings.Contains(r, "! FLAP") {
			flaps++
			if !strings.Contains(r, "2 quick drops") {
				t.Fatalf("flap row: %s", r)
			}
		}
		if strings.Contains(r, " + ") {
			plus3++
		}
	}
	if flaps != 1 {
		t.Fatalf("flap rows = %d: %v", flaps, out)
	}
	if plus3 != 2 { // first two connects visible, third suppressed
		t.Fatalf("connect rows = %d: %v", plus3, out)
	}
}

func TestFlapResetWindowEdges(t *testing.T) {
	has := func(rows []string) bool {
		for _, r := range rows {
			if strings.Contains(r, "FLAP") {
				return true
			}
		}
		return false
	}
	// gap is between drops: drop-to-drop = sess+gap
	if !has(flapRows(55, 4, 2)) { // 59s
		t.Fatal("59s apart should flap")
	}
	if has(flapRows(56, 4, 2)) { // 60s apart resets the counter
		t.Fatal("60s apart should not flap")
	}
}

func TestFlapNotAtThreshold(t *testing.T) {
	e := NewEngine(Config{FlapSecs: 5})
	got := runLines(e,
		dropLine("00:00:11", "1", "192.0.2.1", addrB+".r", addrB, " reason=client_eof age=5.0"),
		dropLine("00:00:12", "2", "192.0.2.1", addrB+".r", addrB, " reason=client_eof age=5.0"))
	for _, r := range got {
		if strings.Contains(r, "FLAP") {
			t.Fatalf("age == threshold folded: %v", got)
		}
	}
	got = runLines(e,
		dropLine("00:00:13", "3", "192.0.2.1", addrB+".r", addrB, " reason=client_eof age=4.9"),
		dropLine("00:00:14", "4", "192.0.2.1", addrB+".r", addrB, " reason=client_eof age=0.8"))
	if len(got) != 2 || !strings.Contains(got[1], "2 quick drops (last session 0s, client_eof)") {
		t.Fatalf("got %q", got)
	}
}

func TestFlapFoldRowRateLimited(t *testing.T) {
	e := NewEngine(Config{})
	var lines []string
	for i := 0; i < 8; i++ {
		lines = append(lines, dropLine(hms(100+i*2), string(rune('1'+i)), "192.0.2.1", addrB+".r", addrB, " age=1.0"))
	}
	var flaps int
	for _, r := range runLines(e, lines...) {
		if strings.Contains(r, "FLAP") {
			flaps++
		}
	}
	if flaps != 1 { // 14s span, one fold row per 30s
		t.Fatalf("flap rows = %d", flaps)
	}
}

func TestNoFlapConfig(t *testing.T) {
	e := NewEngine(Config{NoFlap: true})
	got := runLines(e,
		dropLine("00:00:11", "1", "192.0.2.1", addrB+".r", addrB, " age=1"),
		dropLine("00:00:12", "2", "192.0.2.1", addrB+".r", addrB, " age=1"))
	if len(got) != 2 || strings.Contains(got[1], "FLAP") {
		t.Fatalf("got %q", got)
	}
}

func TestPoolRowSameSecondOnly(t *testing.T) {
	e := NewEngine(Config{})
	got := runLines(e,
		`[2026-01-01 00:00:02.000] Pool:{"runtime":10,"lastupdate":1,"Users":3,"Workers":4,"Idle":1,"Disconnected":0}`,
		`[2026-01-01 00:00:02.000] Pool:{"hashrate1m":"5P","hashrate5m":"4P","hashrate15m":"4P","hashrate1hr":"6P"}`,
		`[2026-01-01 00:00:02.000] Pool:{"diff":9.92,"luck":9.92,"accepted":10,"rejected":100,"SPS1m":2.5}`,
		`[2026-01-01 00:01:02.000] Pool:{"diff":1.0,"accepted":20,"rejected":5000100,"SPS1m":2.5}`)
	if len(got) != 2 {
		t.Fatalf("rows: %q", got)
	}
	want0 := "00:00:02 POOL  5P / 4P / 6P  users 3  workers 4  idle 1  SPS 2.5  luck 9.92%"
	if got[0] != want0 {
		t.Fatalf("got %q want %q", got[0], want0)
	}
	if got[1] != "00:01:02 POOL   (hashrate not logged)  SPS 2.5  rejected +5.00M" {
		t.Fatalf("got %q", got[1])
	}
}

func TestPoolRejectFirstRowNeverAlerts(t *testing.T) {
	e := NewEngine(Config{})
	r := e.Feed(`[2026-01-01 00:00:02.000] Pool:{"diff":1,"rejected":900,"SPS1m":1}`, 0)
	if len(r) != 1 || r[0].Style != StylePool {
		t.Fatalf("%+v", r)
	}
	r = e.Feed(`[2026-01-01 00:01:02.000] Pool:{"diff":1,"rejected":901,"SPS1m":1}`, 1)
	if r[0].Style != StylePoolAlert || r[0].Code != "1;31" {
		t.Fatalf("%+v", r[0])
	}
}

func TestSi(t *testing.T) {
	for in, want := range map[string]string{
		"": "-", "0": "0", "999.9": "999", "1000": "1.00K", "1978744860.9": "1.98G",
		"15000": "15.0K", "250000": "250K", "5000000": "5.00M",
	} {
		if got := si(in); got != want {
			t.Errorf("si(%q) = %q want %q", in, got, want)
		}
	}
}

func TestAddrColourStable(t *testing.T) {
	if AddrColourIndex(addrA) != AddrColourIndex(addrA) {
		t.Fatal("unstable")
	}
	// h = (h*31 + ord) % 9973 over "ab": a=97 -> 97; 97*31+98 = 3105; 3105 % 24 = 9
	if got := AddrColourIndex("ab"); got != 9 {
		t.Fatalf("index = %d", got)
	}
	e := NewEngine(Config{})
	rows := e.Feed(`[2026-01-01 00:00:02.000] User bitcoincash:ab:{"hashrate1m":"1T"}`, 0)
	if rows[0].AddrColour != 9 || rows[0].Code != "38;5;171" {
		t.Fatalf("%+v", rows[0])
	}
}

func TestBlockAndErrorStyles(t *testing.T) {
	e := NewEngine(Config{})
	cases := map[string]Style{
		"BLOCK ACCEPTED!":                        StyleBlockGood,
		"SUBMIT BLOCK RETURNED: inconclusive":    StyleBlockBad,
		"Block hash changed to 0000000000000001": StyleNewBlock,
		"Network diff set to 1234567890.5":       StyleNetDiff,
		"Generator returned failure in update":   StyleError,
		"Stored local workbase with 12 tx":       StyleDim,
		"something ordinary":                     StylePlain,
	}
	for msg, st := range cases {
		r := e.Feed("[2026-01-01 04:00:06.000] "+msg, 0)
		if len(r) != 1 || r[0].Style != st {
			t.Errorf("%q -> %+v", msg, r)
		}
	}
	if r := e.Feed("[2026-01-01 04:00:06.000] ZMQ block hash 00 from endpoint 0", 0); len(r) != 0 {
		t.Errorf("ZMQ shown: %v", r)
	}
}

func TestElapsedHasNoMidnightWrapAfterADay(t *testing.T) {
	e := NewEngine(Config{})
	got := runLines(e,
		authLine("00:00:10", "1", "192.0.2.1", addrA+".r", addrA),
		"[2026-01-02 00:00:12.000] Dropped client 1 192.0.2.1 user bitcoincash:"+addrA+" worker "+addrA+".r")
	if len(got) != 2 || !strings.Contains(got[1], "(24h00m)") {
		t.Fatalf("got %q", got)
	}
}

func TestSplitLineRejoined(t *testing.T) {
	got := runLines(NewEngine(Config{}), readLog(t, "testdata/split.log")...)
	if len(got) != 2 || !strings.Contains(got[0], "7.05P") || !strings.Contains(got[0], "rej 3") {
		t.Fatalf("got %q", got)
	}
}

// monitorOutput runs monitor.sh over a log and returns its visible lines.
func monitorOutput(t *testing.T, log string, env ...string) []string {
	t.Helper()
	for _, tool := range []string{"bash", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
	script, err := filepath.Abs("../../../../../monitor.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Skip("monitor.sh not found")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logs", "cashstratum.log"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CS_MONITOR_FOLLOW=0", "CASHSTRATUM_DIR="+dir, "CS_MONITOR_GROUPS=")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("monitor.sh: %v", err)
	}
	s := strings.TrimSuffix(string(out), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestParityWithMonitorScript(t *testing.T) {
	groups, err := os.ReadFile("testdata/groups.txt")
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("testdata/groups.txt")
	cases := []struct {
		name, log string
		cfg       Config
		env       []string
	}{
		{"regression", "testdata/regression.log", Config{}, nil},
		{"split", "testdata/split.log", Config{}, nil},
		{"rules", "testdata/rules.log", Config{}, nil},
		{"rules-flap10", "testdata/rules.log", Config{FlapSecs: 10}, []string{"CS_MONITOR_FLAP_SECS=10"}},
		{"rules-groups", "testdata/rules.log", Config{Groups: ParseGroups(string(groups))}, []string{"CS_MONITOR_GROUPS=" + abs}},
	}
	compared := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := monitorOutput(t, tc.log, tc.env...)
			got := runLines(NewEngine(tc.cfg), readLog(t, tc.log)...)
			if len(got) != len(want) {
				t.Errorf("row count: go %d, monitor.sh %d", len(got), len(want))
			}
			for i := 0; i < len(got) && i < len(want); i++ {
				if got[i] != want[i] {
					t.Errorf("row %d:\n go: %q\n sh: %q", i, got[i], want[i])
				}
			}
			compared += len(want)
		})
	}
	t.Logf("compared %d lines", compared)
}
