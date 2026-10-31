package poolfmt

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseHashrate(t *testing.T) {
	ok := map[string]float64{"0": 0, "301": 301, "679M": 679e6, "1.02G": 1.02e9, "6.41T": 6.41e12,
		"4.97P": 4.97e15, "28P": 28e15, "379K": 379e3, "1.5E": 1.5e18, " 5T ": 5e12, "0.5": 0.5}
	for in, want := range ok {
		got, err := ParseHashrate(in)
		if err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"", "K", "abc", "1.2.3M", "-5T", "inf", "NaN", "0x10", "1e3", "5 T", "5Z", "\xff"} {
		if _, err := ParseHashrate(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func FuzzParseHashrate(f *testing.F) {
	for _, s := range []string{"301", "679M", "4.97P", "", "K", "1e3", "\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if v, err := ParseHashrate(s); err == nil && (v < 0 || v != v) {
			t.Fatalf("bad value %v", v)
		}
	})
}

func TestAddressHelpers(t *testing.T) {
	cases := map[string]string{"bitcoincash:qqdummy": "qqdummy", "qqdummy": "qqdummy", "1Legacy": "1Legacy",
		"ecash:qq": "qq", "": "", ":": ":", "Bitcoincash:q": "Bitcoincash:q"}
	for in, want := range cases {
		if got := NormalizeAddress(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	a, r := SplitWorker("bitcoincash:qq.rig.01")
	if a != "bitcoincash:qq" || r != "rig.01" {
		t.Error(a, r)
	}
	if a, r := SplitWorker("qq"); a != "qq" || r != "" {
		t.Error(a, r)
	}
}

func FuzzNormalizeAddress(f *testing.F) {
	for _, s := range []string{"bitcoincash:qq", "qq", "", ":", "\xff:"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n := NormalizeAddress(s)
		if len(n) > len(s) {
			t.Fatal("grew")
		}
		_, _ = SplitWorker(s)
	})
}

const userFile = `{
    "hashrate1m": "1.5T",
    "hashrate5m": "1T",
    "hashrate1hr": "900G",
    "hashrate1d": "0",
    "hashrate7d": "10K",
    "lastshare": 1767323045,
    "workers": 2,
    "shares": 100,
    "roundshares": 50,
    "bestshare": 1978744860.9763112,
    "luck": 12.5,
    "networkdiff": 1000.5,
    "bestever": 999,
    "authorised": 1767000000,
    "worker": [
        {"workername": "qqdummy.rig01", "hashrate1m": "800G", "hashrate5m": "0", "hashrate1hr": "0", "hashrate1d": "0", "hashrate7d": "0",
         "lastshare": 1767323000, "shares": 60, "bestshare": 5.5, "bestever": 7},
        {"workername": "qqdummy.rig02", "hashrate1m": "0", "hashrate5m": "0", "hashrate1hr": "0", "hashrate1d": "0", "hashrate7d": "0",
         "lastshare": 1, "shares": 0, "bestshare": 0, "bestever": 0,
         "rejected": 9, "rejectedcount": 3, "rejects": {"stale": 2, "duplicate": 1}, "lastreject": 5, "lastrejectreason": "stale",
         "rejectrate1m": "2", "rejectrate5m": "0", "rejectrate1hr": "0",
         "connects": 4, "disconnects": 3, "lastdisconnect": 99, "lastdisconnectreason": "peer_eof"}
    ]
}
`

func TestParseUserFile(t *testing.T) {
	u, err := ParseUserFile(strings.NewReader(userFile))
	if err != nil {
		t.Fatal(err)
	}
	if u.Hashrate1m != 1.5e12 || u.Workers.V != 2 || u.BestShare.V != 1978744860.9763112 || len(u.Worker) != 2 || u.Reject.Present {
		t.Errorf("%+v", u.UserStats)
	}
	w1, w2 := u.Worker[0], u.Worker[1]
	if w1.Name != "qqdummy.rig01" || w1.Reject.Present || w1.Connects.OK || w1.Hashrate1m != 800e9 {
		t.Errorf("%+v", w1)
	}
	if !w2.Reject.Present || w2.Reject.Buckets.Stale != 2 || w2.Reject.Rate1m.V != 2 || !w2.Reject.Rate5m.OK ||
		w2.Disconnects.V != 3 || w2.LastDisconnectReason.V != "peer_eof" || w2.Reject.LastReason.V != "stale" {
		t.Errorf("%+v", w2)
	}
	// Absent optional keys are not zero-valued presence.
	if w1.Disconnects.OK || w1.LastDisconnect.OK {
		t.Error("presence")
	}

	for name, in := range map[string]string{
		"empty": "", "whitespace": " \n", "torn": userFile[:200], "not json": "hello", "array": "[]",
		"wrong type": `{"shares":"x"}`,
	} {
		if _, err := ParseUserFile(strings.NewReader(in)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := ParseUserFile(strings.NewReader("")); !errors.Is(err, ErrEmpty) {
		t.Error("ErrEmpty")
	}
	if _, err := ParseUserFile(nil); err == nil {
		t.Error("nil reader")
	}
	// A worker-less minimal file parses.
	if u, err := ParseUserFile(strings.NewReader(`{"shares":1}`)); err != nil || u.Shares != 1 || len(u.Worker) != 0 {
		t.Error(u, err)
	}
}

func FuzzParseUserFile(f *testing.F) {
	f.Add(userFile)
	f.Add(userFile[:300])
	f.Add("")
	f.Add(`{"worker":[null,{"rejects":3}]}`)
	f.Fuzz(func(t *testing.T, s string) { _, _ = ParseUserFile(strings.NewReader(s)) })
}

const poolStatus = `{"runtime":311776,"lastupdate":1767323045,"Users":3,"Workers":9,"Idle":1,"Disconnected":5}
{"hashrate1m":"4.97P","hashrate5m":"4P","hashrate15m":"3P","hashrate1hr":"2P","hashrate6hr":"1P","hashrate1d":"379K","hashrate7d":"0"}
{"diff":131.93,"luck":131.93,"accepted":694727436102,"rejected":12,"bestshare":4927660663172,"SPS1m":1.5,"SPS5m":2,"SPS15m":3,"SPS1h":4}
`

func TestParsePoolStatus(t *testing.T) {
	p, err := ParsePoolStatus(strings.NewReader(poolStatus))
	if err != nil {
		t.Fatal(err)
	}
	if p.Runtime != 311776 || p.Hashrate1m != 4.97e15 || p.Diff != 131.93 || p.Accepted != 694727436102 || p.SPS1h != 4 {
		t.Errorf("%+v", p)
	}
	// SV2 extra lines are tolerated.
	if _, err := ParsePoolStatus(strings.NewReader(poolStatus + `{"x":1}` + "\r\n")); err != nil {
		t.Error(err)
	}
	lines := strings.Split(strings.TrimSpace(poolStatus), "\n")
	bad := map[string]string{
		"empty": "", "one line": lines[0], "two lines": lines[0] + "\n" + lines[1] + "\n",
		"torn last": lines[0] + "\n" + lines[1] + "\n" + lines[2][:30],
		"dup":       lines[0] + "\n" + lines[0] + "\n" + lines[0],
	}
	for n, in := range bad {
		if _, err := ParsePoolStatus(strings.NewReader(in)); err == nil {
			t.Errorf("%s: no error", n)
		}
	}
	if _, err := ParsePoolStatus(strings.NewReader("")); !errors.Is(err, ErrEmpty) {
		t.Error("ErrEmpty")
	}
}

func FuzzParsePoolStatus(f *testing.F) {
	f.Add(poolStatus)
	f.Add(poolStatus[:100])
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) { _, _ = ParsePoolStatus(strings.NewReader(s)) })
}

const shareLine = `{"workinfoid":7700840868118277726,"clientid":5,"enonce1":"deadbeef","nonce2":"00000001","nonce":"cafebabe","ntime":"69561234","diff":5000.0,"sdiff":4927660663172.5,"hash":"` + h2 + `","result":true,"error":"Valid","errn":0,"createdate":"1767323045,123456","createby":"code","createcode":"parse_submit","createinet":"0.0.0.0:3333","workername":"qqdummy.rig01","username":"bitcoincash:qqdummy","address":"192.0.2.7","agent":"Miner/1.0|x","version_mask":"1fffe000"}`

func TestParseSharelogLine(t *testing.T) {
	s, err := ParseSharelogLine(shareLine)
	if err != nil {
		t.Fatal(err)
	}
	if s.WorkInfoID != 7700840868118277726 || !s.Accepted() || s.Diff != 5000 || s.WorkerName != "qqdummy.rig01" || s.VersionMask != "1fffe000" {
		t.Errorf("%+v", s)
	}
	if !s.IsBlock(4.9e12) || s.IsBlock(5e12) || s.IsBlock(0) {
		t.Error("IsBlock")
	}
	rej, err := ParseSharelogLine(`{"sdiff":9e99,"result":false,"error":"Above target","errn":5}`)
	if err != nil || rej.IsBlock(1) || rej.Errn != 5 {
		t.Error("rejected share", rej, err)
	}
	for _, in := range []string{"", "  ", "{", shareLine[:50], `{"result":"yes"}`, "null1"} {
		if _, err := ParseSharelogLine(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	if h, w, ok := ParseSharelogName("000ee0f3", "6ae3d05eb7b2145e.sharelog"); !ok || h != 0xee0f3 || w != 0x6ae3d05eb7b2145e {
		t.Error(h, w, ok)
	}
	for _, p := range [][2]string{{"ee0f3", "6ae3d05eb7b2145e.sharelog"}, {"000ee0f3", "6ae3d05eb7b2145e"}, {"000EE0F3", "x.sharelog"}} {
		if _, _, ok := ParseSharelogName(p[0], p[1]); ok {
			t.Errorf("%v accepted", p)
		}
	}
}

func FuzzParseSharelogLine(f *testing.F) {
	f.Add(shareLine)
	f.Add(shareLine[:80])
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		if sh, err := ParseSharelogLine(s); err == nil {
			_ = sh.IsBlock(1)
		}
	})
}

func TestParseSolvedHeights(t *testing.T) {
	in := "# cashstratum solve index v1: verified baseline\n000ed3ce\n000ed3cf\r\n\nnothex!!\n000EDDDD\n000ee0f3\n"
	got, err := ParseSolvedHeights(strings.NewReader(in))
	if err != nil || len(got) != 3 || got[0] != 0xed3ce || got[2] != 0xee0f3 {
		t.Errorf("%v %v", got, err)
	}
	if _, err := ParseSolvedHeights(strings.NewReader("")); !errors.Is(err, ErrEmpty) {
		t.Error("ErrEmpty")
	}
	if got, err := ParseSolvedHeights(strings.NewReader("# marker only\n")); err != nil || len(got) != 0 {
		t.Error(got, err)
	}
	if _, err := ParseSolvedHeights(strings.NewReader(strings.Repeat("a", 5<<20))); !errors.Is(err, ErrTooLong) {
		t.Error("too long")
	}
}

func FuzzParseSolvedHeights(f *testing.F) {
	f.Add("# m\n000ed3ce\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) { _, _ = ParseSolvedHeights(strings.NewReader(s)) })
}

const solveHdr = `# CashStratum solve context — preserved 2026-01-02 23:30:02 UTC
# source:   /srv/pool/logs/cashstratum.log (anchor line 100)
# height:   975091
# hash:     ` + h2 + ` (ZMQ-seen: ` + h2 + `)
# finder:   bitcoincash:qqdummy.rig01
# chain:    confirmations=12 at preserve time -> ACCEPTED
# window:   lines 1..200

[2026-01-02 03:04:05.678] BLOCK ACCEPTED!
`

func TestParseSolveHeader(t *testing.T) {
	h, err := ParseSolveHeader(strings.NewReader(solveHdr))
	if err != nil {
		t.Fatal(err)
	}
	if h.Height != 975091 || h.Hash != h2 || h.ZMQHash != h2 || h.Finder != "bitcoincash:qqdummy.rig01" ||
		h.Confirmations.V != 12 || h.Outcome != "ACCEPTED" || h.Window != "lines 1..200" || h.Preserved != "2026-01-02 23:30:02 UTC" {
		t.Errorf("%+v", h)
	}
	o := strings.Replace(strings.Replace(solveHdr, "ZMQ-seen: "+h2, "ZMQ-seen: none", 1),
		"confirmations=12 at preserve time -> ACCEPTED", "confirmations=-1 at preserve time -> ORPHANED(conf=-1)", 1)
	h, err = ParseSolveHeader(strings.NewReader(o))
	if err != nil || h.ZMQHash != "" || h.Outcome != "ORPHANED" || h.OutcomeDetail != "conf=-1" || h.Confirmations.V != -1 {
		t.Errorf("%+v %v", h, err)
	}
	for _, in := range []string{"", "no header\n", "# height: abc\n"} {
		if _, err := ParseSolveHeader(strings.NewReader(in)); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	if height, hash, ok := ParseSolveFilename("975091-" + h2 + ".log"); !ok || height != 975091 || hash != h2 {
		t.Error("filename")
	}
	for _, n := range []string{"INDEX.tsv", "SHA256SUMS", "1-abc.log", "x-" + h2 + ".log"} {
		if _, _, ok := ParseSolveFilename(n); ok {
			t.Errorf("%q accepted", n)
		}
	}
}

func FuzzParseSolveHeader(f *testing.F) {
	f.Add(solveHdr)
	f.Add(solveHdr[:90])
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseSolveHeader(strings.NewReader(s))
		_, _, _ = ParseSolveFilename(s)
	})
}

const solvesIndex = "height\tblockhash\ttimestamp\toutcome\tfinder\tconfirmations\tcontext_file\n" +
	"975091\t" + h2 + "\t2026-01-02 03:04:05.678\tACCEPTED\tqqdummy.rig01\t12\t975091-" + h2 + ".log\r\n" +
	"bad\trow\n" +
	"975092\t" + h1 + "\tnot-a-time\tORPHANED\tqqdummy\t-1\t\n"

func TestParseSolvesIndex(t *testing.T) {
	es, err := ParseSolvesIndexIn(strings.NewReader(solvesIndex), time.UTC)
	if err != nil || len(es) != 2 {
		t.Fatal(es, err)
	}
	if es[0].Height != 975091 || !es[0].HasTime || es[0].Confirmations.V != 12 || es[0].Outcome != "ACCEPTED" ||
		es[0].ContextFile != "975091-"+h2+".log" || es[1].HasTime || es[1].Confirmations.V != -1 {
		t.Errorf("%+v", es)
	}
	if es, err := ParseSolvesIndex(strings.NewReader("")); err != nil || len(es) != 0 {
		t.Error(es, err)
	}
}

func FuzzParseSolvesIndex(f *testing.F) {
	f.Add(solvesIndex)
	f.Add("a\tb\tc\td\te\tf")
	f.Fuzz(func(t *testing.T, s string) { _, _ = ParseSolvesIndex(strings.NewReader(s)) })
}
