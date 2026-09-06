package ckconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return p
}

func TestCandidates(t *testing.T) {
	// The deployed layout: conf beside the logdir, not inside it.
	got := Candidates("/mnt/data4tb/bch/ckpool/logs/ckpool.log")
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

	// Real ckpool.conf shape: btcd is an array, first usable entry wins.
	good := write(t, dir, "good.conf", `{
		"btcd": [{"url": "127.0.0.1:8332", "auth": "bchadmin", "pass": "s3cret", "notify": true}],
		"bchaddress": "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS",
		"mindiff_overrides": {"nicehash": 500000}
	}`)
	ep := LoadBtcdRPC(good)
	if ep == nil {
		t.Fatal("LoadBtcdRPC returned nil for a well-formed conf")
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
		if ep := LoadBtcdRPC(write(t, dir, name, body)); ep != nil {
			t.Errorf("%s: got %+v, want nil", name, *ep)
		}
	}
	if ep := LoadBtcdRPC(filepath.Join(dir, "does-not-exist.conf")); ep != nil {
		t.Errorf("missing conf: got %+v, want nil", *ep)
	}
}

// LoadUpdateInterval: the key set, absent (falls back to
// DefaultUpdateInterval), and other non-fatal edge cases (zero, missing
// file, malformed JSON) all degrade the same way LoadBtcdRPC does.
func TestLoadUpdateInterval(t *testing.T) {
	dir := t.TempDir()

	withKey := write(t, dir, "with-key.conf", `{"update_interval": 15, "btcd": []}`)
	if got := LoadUpdateInterval(withKey); got != 15*time.Second {
		t.Errorf("with key set: got %s, want 15s", got)
	}

	withoutKey := write(t, dir, "without-key.conf", `{"btcd": [{"url": "127.0.0.1:8332"}]}`)
	if got := LoadUpdateInterval(withoutKey); got != DefaultUpdateInterval {
		t.Errorf("key absent: got %s, want default %s", got, DefaultUpdateInterval)
	}

	zeroKey := write(t, dir, "zero-key.conf", `{"update_interval": 0}`)
	if got := LoadUpdateInterval(zeroKey); got != DefaultUpdateInterval {
		t.Errorf("key zero: got %s, want default %s", got, DefaultUpdateInterval)
	}

	if got := LoadUpdateInterval(filepath.Join(dir, "does-not-exist.conf")); got != DefaultUpdateInterval {
		t.Errorf("missing conf: got %s, want default %s", got, DefaultUpdateInterval)
	}

	malformed := write(t, dir, "malformed.conf", `{ not json`)
	if got := LoadUpdateInterval(malformed); got != DefaultUpdateInterval {
		t.Errorf("malformed conf: got %s, want default %s", got, DefaultUpdateInterval)
	}
}

// ResolveZMQEndpoint's precedence mirrors ckpool's own (src/stratifier.c:
// 8883-8888): per-btcd zmqnotify beats the top-level zmqblock fallback.
// This is load-bearing -- production's ckpool.conf has ONLY
// btcd[0].zmqnotify and no top-level zmqblock key at all.
func TestResolveZMQEndpoint(t *testing.T) {
	t.Run("resolved from btcd[].zmqnotify", func(t *testing.T) {
		conf := Conf{
			Btcd: []BtcdEndpoint{
				{URL: "127.0.0.1:8332", ZmqNotify: "tcp://127.0.0.1:28332"},
			},
		}
		got, err := ResolveZMQEndpoint(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "tcp://127.0.0.1:28332" {
			t.Errorf("got %q, want btcd[0].zmqnotify", got)
		}
	})

	t.Run("falls back to top-level zmqblock when no btcd entry has zmqnotify", func(t *testing.T) {
		conf := Conf{
			Btcd:     []BtcdEndpoint{{URL: "127.0.0.1:8332"}},
			ZmqBlock: "tcp://127.0.0.1:38332",
		}
		got, err := ResolveZMQEndpoint(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "tcp://127.0.0.1:38332" {
			t.Errorf("got %q, want top-level zmqblock", got)
		}
	})

	t.Run("both set: zmqnotify wins", func(t *testing.T) {
		conf := Conf{
			Btcd: []BtcdEndpoint{
				{URL: "127.0.0.1:8332", ZmqNotify: "tcp://127.0.0.1:28332"},
			},
			ZmqBlock: "tcp://127.0.0.1:38332",
		}
		got, err := ResolveZMQEndpoint(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "tcp://127.0.0.1:28332" {
			t.Errorf("got %q, want btcd[0].zmqnotify to win over zmqblock", got)
		}
	})

	t.Run("neither set: error names both keys", func(t *testing.T) {
		conf := Conf{Btcd: []BtcdEndpoint{{URL: "127.0.0.1:8332"}}}
		_, err := ResolveZMQEndpoint(conf)
		if err == nil {
			t.Fatal("want error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "zmqnotify") || !strings.Contains(msg, "zmqblock") {
			t.Errorf("error %q must name both zmqnotify and zmqblock", msg)
		}
	})

	// beast's shape: BOTH btcd[0].zmqnotify AND a top-level zmqblock are
	// present -- zmqnotify must still win, matching ckpool's own precedence.
	t.Run("beast shape: both present, zmqnotify wins", func(t *testing.T) {
		conf := Conf{
			Btcd: []BtcdEndpoint{
				{URL: "127.0.0.1:8332", Auth: "user", Pass: "pass", ZmqNotify: "tcp://127.0.0.1:28332"},
			},
			ZmqBlock: "tcp://127.0.0.1:28332",
		}
		got, err := ResolveZMQEndpoint(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "tcp://127.0.0.1:28332" {
			t.Errorf("got %q, want zmqnotify", got)
		}
	})

	// production's shape: btcd[0].zmqnotify set, NO top-level zmqblock key
	// at all -- this is the case that would break live if precedence were
	// reversed or zmqblock were required. Fixture genuinely omits the key
	// (not present-but-empty) by decoding real JSON rather than a struct
	// literal.
	t.Run("production shape: zmqnotify only, zmqblock key absent", func(t *testing.T) {
		raw := `{
			"btcd": [{"url": "127.0.0.1:8332", "auth": "prod", "pass": "prodpass", "zmqnotify": "tcp://127.0.0.1:28332"}]
		}`
		var conf Conf
		if err := json.Unmarshal([]byte(raw), &conf); err != nil {
			t.Fatalf("unmarshal fixture: %v", err)
		}
		if conf.ZmqBlock != "" {
			t.Fatalf("fixture must omit zmqblock entirely, got %q", conf.ZmqBlock)
		}
		got, err := ResolveZMQEndpoint(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "tcp://127.0.0.1:28332" {
			t.Errorf("got %q, want btcd[0].zmqnotify", got)
		}
	})

	// The repo's own root ckpool.conf fixture: no per-btcd zmqnotify key at
	// all, only a top-level zmqblock -- exercises the fallback path against
	// a real file rather than a hand-built literal. The fixture carries a
	// trailing "Comments from here on are ignored." line after the closing
	// brace (ckpool.conf's own documented convention), so decode only the
	// first JSON value rather than json.Unmarshal-ing the whole file.
	t.Run("repo ckpool.conf fixture: falls back to zmqblock", func(t *testing.T) {
		fixturePath := repoRootConfPath(t)
		f, err := os.Open(fixturePath)
		if err != nil {
			t.Skipf("no repo ckpool.conf fixture at %s, skipping: %v", fixturePath, err)
			return
		}
		defer f.Close()
		var conf Conf
		if err := json.NewDecoder(f).Decode(&conf); err != nil {
			t.Fatalf("decode %s: %v", fixturePath, err)
		}
		got, err := ResolveZMQEndpoint(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != conf.ZmqBlock {
			t.Errorf("got %q, want the fixture's top-level zmqblock %q", got, conf.ZmqBlock)
		}
	})
}

// repoRootConfPath resolves the repo root ckpool.conf relative to this test
// file's own directory (api/internal/ckconf), independent of the working
// directory `go test` is invoked from.
func repoRootConfPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// api/internal/ckconf -> repo root is three levels up.
	return filepath.Join(wd, "..", "..", "..", "ckpool.conf")
}
