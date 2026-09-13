package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cashstratumapi/internal/ckconf"
)

func TestShortWorkerName(t *testing.T) {
	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"

	cases := []struct {
		name       string
		workername string
		username   string
		want       string
	}{
		{"dot separator", addr + ".nh", addr, "nh"},
		{"underscore separator", addr + "_nh", addr, "nh"},
		{"dots inside the worker segment are kept", addr + ".rig.1", addr, "rig.1"},
		{"no worker segment yields empty, not the address", addr, addr, ""},
		// The stats file is named after one CashAddr spelling while the
		// worker line may carry the other; the separator scan must still win.
		{"username spelled differently to workername", addr + ".nh", "qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u", "nh"},
		{"username unknown", addr + ".nh", "", "nh"},
		{"legacy base58 address", "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS.testrig", "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", "testrig"},
		{"empty worker segment after separator", addr + ".", addr, ""},
		{"empty workername", "", addr, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortWorkerName(tc.workername, tc.username); got != tc.want {
				t.Errorf("shortWorkerName(%q, %q) = %q, want %q", tc.workername, tc.username, got, tc.want)
			}
		})
	}
}

// A malformed or unexpected shape must be passed through untouched rather
// than panicking: /user-file relays a file whose schema ckpool owns.
func TestAnnotateUserFileWorkersToleratesOddShapes(t *testing.T) {
	for _, raw := range []string{
		`{"worker": "not-an-array"}`,
		`{"worker": [null, 3, "x"]}`,
		`{"worker": [{"workername": 7}]}`,
		`{"shares": 1}`,
		`[]`,
		`"a string file"`,
	} {
		var content interface{}
		if err := json.Unmarshal([]byte(raw), &content); err != nil {
			t.Fatalf("fixture %s: %v", raw, err)
		}
		annotateUserFileWorkers(content, "addr") // must not panic
	}
}

func TestHandleUserFileAddsShortWorkerName(t *testing.T) {
	dir := useTempUserLogDir(t)
	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"

	// Shape copied from a real logs/users file, trimmed to what matters here.
	body := `{
		"shares": 145146117994,
		"luck": 32.29,
		"worker": [
			{"workername": "` + addr + `.nh", "shares": 145146117994},
			{"workername": "` + addr + `", "shares": 12}
		],
		"workers": 0
	}`
	if err := os.WriteFile(filepath.Join(dir, addr), []byte(body), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/user-file?user="+addr, nil)
	rec := httptest.NewRecorder()
	handleUserFile(rec, req)

	var resp struct {
		Username string `json:"username"`
		Content  struct {
			Worker []struct {
				Workername string `json:"workername"`
				Worker     string `json:"worker"`
			} `json:"worker"`
		} `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(resp.Content.Worker) != 2 {
		t.Fatalf("worker entries = %d, want 2", len(resp.Content.Worker))
	}
	if got := resp.Content.Worker[0].Worker; got != "nh" {
		t.Errorf(`worker[0].worker = %q, want "nh"`, got)
	}
	// Additive only: the full name callers already match on is untouched.
	if got := resp.Content.Worker[0].Workername; got != addr+".nh" {
		t.Errorf("worker[0].workername = %q, want it left verbatim as %q", got, addr+".nh")
	}
	if got := resp.Content.Worker[1].Worker; got != "" {
		t.Errorf(`worker[1].worker = %q, want "" for a suffix-less miner`, got)
	}
}

func TestCkpoolConfCandidates(t *testing.T) {
	// The deployed layout: conf beside the logdir, not inside it.
	got := ckconf.Candidates("/mnt/data4tb/bch/ckpool/logs/ckpool.log")
	want := []string{
		"/mnt/data4tb/bch/ckpool/cashstratum.conf",
		"/mnt/data4tb/bch/ckpool/logs/cashstratum.conf",
		"/mnt/data4tb/bch/ckpool/ckpool.conf",
		"/mnt/data4tb/bch/ckpool/logs/ckpool.conf",
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadBtcdRPC(t *testing.T) {
	dir := t.TempDir()

	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return p
	}

	// Real ckpool.conf shape: btcd is an array, first usable entry wins.
	good := write("good.conf", `{
		"btcd": [{"url": "127.0.0.1:8332", "auth": "bchadmin", "pass": "s3cret", "notify": true}],
		"bchaddress": "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS",
		"mindiff_overrides": {"nicehash": 500000}
	}`)
	ep := ckconf.LoadBtcdRPC(good)
	if ep == nil {
		t.Fatal("loadBtcdRPC returned nil for a well-formed conf")
	}
	if ep.URL != "127.0.0.1:8332" || ep.Auth != "bchadmin" || ep.Pass != "s3cret" {
		t.Errorf("endpoint = %+v, want the first btcd entry", *ep)
	}

	// Every failure mode is nil, never fatal: the API must still serve logs
	// on a host where the conf is missing, unreadable, or node-less.
	for name, body := range map[string]string{
		"malformed.conf": `{ not json`,
		"no-btcd.conf":   `{"bchaddress": "1AGQ"}`,
		"empty-url.conf": `{"btcd": [{"url": "", "auth": "a", "pass": "b"}]}`,
	} {
		if ep := ckconf.LoadBtcdRPC(write(name, body)); ep != nil {
			t.Errorf("%s: got %+v, want nil", name, *ep)
		}
	}
	if ep := ckconf.LoadBtcdRPC(filepath.Join(dir, "does-not-exist.conf")); ep != nil {
		t.Errorf("missing conf: got %+v, want nil", *ep)
	}
}

// /shares derives `worker` at read time. The sharelog line has no such
// field, so a line that carries one must not be able to dictate it.
func TestSharesDerivesWorkerField(t *testing.T) {
	base := useTempSharelogDir(t)
	resetShareCache(t)

	const addr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"
	writeSharelog(t, base, 500, 1, []string{
		marshalShare(t, ShareRecord{
			WorkinfoID: 1,
			Workername: addr + ".nh",
			Username:   addr,
			Worker:     "spoofed-by-the-log-line",
			Result:     true,
		}),
		marshalShare(t, ShareRecord{
			WorkinfoID: 2,
			Workername: addr,
			Username:   addr,
			Result:     true,
		}),
	}, true)

	resp := getShares(t, map[string]string{"limit": "10"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
	if len(resp.Shares) != 2 {
		t.Fatalf("shares = %d, want 2", len(resp.Shares))
	}
	if got := resp.Shares[0].Worker; got != "nh" {
		t.Errorf(`shares[0].worker = %q, want "nh" (derived, not read from the line)`, got)
	}
	if got := resp.Shares[0].Workername; got != addr+".nh" {
		t.Errorf("shares[0].workername = %q, want it left verbatim", got)
	}
	if got := resp.Shares[1].Worker; got != "" {
		t.Errorf(`shares[1].worker = %q, want "" for a suffix-less miner`, got)
	}
}
