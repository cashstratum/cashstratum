package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "charm.land/bubbles/v2/viewport"
	_ "charm.land/lipgloss/v2"
	_ "github.com/charmbracelet/colorprofile"
	_ "github.com/charmbracelet/harmonica"
	_ "github.com/charmbracelet/x/exp/teatest/v2"

	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/fixturegen"
)

func TestParseFlags(t *testing.T) {
	o, err := parseFlags([]string{"--demo", "--notify=bell", "--root", "/x"})
	if err != nil || !o.demo || o.notify != "bell" || o.root != "/x" {
		t.Fatalf("unexpected: %+v %v", o, err)
	}
	if _, err := parseFlags([]string{"--notify=nope"}); err == nil {
		t.Fatal("expected error for bad notify")
	}
}

func TestRunWithoutRootExits2(t *testing.T) {
	var out, errb strings.Builder
	code := run([]string{"--root", filepath.Join(t.TempDir(), "missing")}, func(string) string { return "" }, &out, &errb)
	if code != 2 || !strings.Contains(errb.String(), "pool directory not found") || out.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
}

func TestSnapshotOnce(t *testing.T) {
	dir := t.TempDir()
	if _, err := fixturegen.WriteTree(dir, 7, true); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var out, errb strings.Builder
	code := run([]string{"--root", dir, "--snapshot-once"}, func(string) string { return "" }, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errb.String())
	}
	var s ui.Snapshot
	if err := json.Unmarshal([]byte(out.String()), &s); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if len(s.Workers) == 0 || len(s.Blocks) == 0 || s.Pool.Hashrate1m <= 0 {
		t.Fatalf("snapshot does not reflect the tree: %d workers, %d blocks", len(s.Workers), len(s.Blocks))
	}
	if errb.Len() != 0 {
		t.Fatalf("unexpected stderr %q", errb.String())
	}
}

func TestDemoSnapshotOnce(t *testing.T) {
	var out, errb strings.Builder
	if code := run([]string{"--demo", "--snapshot-once"}, func(string) string { return "" }, &out, &errb); code != 0 || !json.Valid([]byte(out.String())) {
		t.Fatalf("code=%d stderr=%q", code, errb.String())
	}
}

func TestRunVersion(t *testing.T) {
	var out, errb strings.Builder
	if code := run([]string{"--version"}, func(string) string { return "" }, &out, &errb); code != 0 || !strings.HasPrefix(out.String(), "cashtop ") {
		t.Fatalf("code=%d stdout=%q", code, out.String())
	}
}

// With a node, --snapshot-wait lets enrichment fill the rewards before the
// snapshot prints.
func TestSnapshotWaitFillsRewards(t *testing.T) {
	dir := t.TempDir()
	tr, err := fixturegen.WriteTree(dir, 7, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct{ Method string }
		_ = json.Unmarshal(b, &req)
		switch req.Method {
		case "getblockchaininfo":
			fmt.Fprint(w, `{"result":{"chain":"main","blocks":900000,"bestblockhash":"aa","difficulty":1000000},"error":null}`)
		case "getblockhash":
			fmt.Fprint(w, `{"result":"bb","error":null}`)
		case "getblock":
			fmt.Fprint(w, `{"result":{"tx":["cb"]},"error":null}`)
		case "getrawtransaction":
			fmt.Fprint(w, `{"result":{"vout":[{"value":3.125}]},"error":null}`)
		default:
			fmt.Fprint(w, `{"result":{},"error":null}`)
		}
	}))
	defer srv.Close()
	conf := filepath.Join(dir, "ckpool.conf")
	body := fmt.Sprintf(`{"btcd":[{"url":%q,"auth":"u","pass":"p"}]}`, srv.URL)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb strings.Builder
	code := run([]string{"--root", dir, "--conf", conf, "--snapshot-once", "--snapshot-wait=2s"},
		func(string) string { return "" }, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errb.String())
	}
	var s ui.Snapshot
	if err := json.Unmarshal([]byte(out.String()), &s); err != nil {
		t.Fatal(err)
	}
	if n := acceptedRewards(t, s, 312500000); n == 0 {
		t.Fatal("no accepted blocks")
	}

	// A second run with the node gone serves the same rewards from the cache.
	srv.Close()
	f, err := os.OpenFile(tr.LogPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	g := fixturegen.New(21, fixturegen.FixtureStart.Add(24*time.Hour))
	for _, l := range g.BlockCluster(950000, fixturegen.FullWorker(1, 0), "SV1") {
		fmt.Fprintln(f, l)
	}
	f.Close()
	out.Reset()
	errb.Reset()
	if code := run([]string{"--root", dir, "--conf", conf, "--snapshot-once"},
		func(string) string { return "" }, &out, &errb); code != 0 {
		t.Fatalf("second run code=%d stderr=%q", code, errb.String())
	}
	var s2 ui.Snapshot
	if err := json.Unmarshal([]byte(out.String()), &s2); err != nil {
		t.Fatal(err)
	}
	if n := acceptedRewards(t, s2, 312500000); n == 0 {
		t.Fatal("no accepted blocks on the second run")
	}
}

// acceptedRewards fails unless every accepted block carries want, and returns
// how many accepted blocks there are.
func acceptedRewards(t *testing.T, s ui.Snapshot, want int64) int {
	t.Helper()
	n := 0
	for _, b := range s.Blocks {
		if b.Status != ui.BlockRejected && b.Status != ui.BlockOrphaned && b.Height != 950000 {
			n++
			if b.Reward != want {
				t.Fatalf("block %d reward %d", b.Height, b.Reward)
			}
		}
	}
	return n
}

func TestDemoProfileFlag(t *testing.T) {
	for args, want := range map[string]string{"--demo": "", "--demo=showcase": "showcase", "--demo=true": ""} {
		o, err := parseFlags([]string{args})
		if err != nil || !o.demo || o.demoProfile != want {
			t.Fatalf("%s: %+v %v", args, o, err)
		}
	}
	if o, err := parseFlags([]string{"--demo=false"}); err != nil || o.demo {
		t.Fatalf("--demo=false: %+v %v", o, err)
	}
	if _, err := parseFlags([]string{"--demo=other"}); err == nil {
		t.Fatal("unknown profile must fail")
	}
	if _, err := parseFlags([]string{"--render-frame=feed"}); err == nil {
		t.Fatal("--render-frame without --demo must fail")
	}
	if _, err := parseFlags([]string{"--demo", "--theme=neon"}); err == nil {
		t.Fatal("unknown theme must fail")
	}
}

func TestRenderFrameFlag(t *testing.T) {
	var out, errb strings.Builder
	code := run([]string{"--demo=showcase", "--render-frame=workers", "--size=100x30", "--theme=mono"}, func(string) string { return "" }, &out, &errb)
	if code != 0 || errb.Len() != 0 || strings.Count(out.String(), "\n") != 30 || strings.Contains(out.String(), "\x1b[?") {
		t.Fatalf("code=%d lines=%d stderr=%q", code, strings.Count(out.String(), "\n"), errb.String())
	}
	for _, args := range [][]string{{"--demo", "--render-frame=nope"}, {"--demo", "--render-frame=feed", "--size=ax1"}} {
		if code := run(args, func(string) string { return "" }, &out, &errb); code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
	}
}
