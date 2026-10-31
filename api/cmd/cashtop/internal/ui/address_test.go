package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/poolfmt"
)

var addrNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

type addrProv struct{ d AddressData }

func (addrProv) Snapshot() Snapshot               { return Snapshot{} }
func (addrProv) DrillDown(int) (DrillDown, error) { return DrillDown{}, nil }
func (p addrProv) Address(string) AddressData     { return p.d }

type bareProv struct{}

func (bareProv) Snapshot() Snapshot               { return Snapshot{} }
func (bareProv) DrillDown(int) (DrillDown, error) { return DrillDown{}, nil }

func addrShare(addr, worker string, at time.Time, diff, sdiff float64, ok bool, errs string) poolfmt.Share {
	return poolfmt.Share{
		Diff: diff, SDiff: sdiff, Result: ok, Error: errs, UserName: addr, Address: fixturegen.IP(9), WorkerName: worker,
		Agent: "Miner/1.0", CreateDate: strconv.FormatInt(at.Unix(), 10) + ",0",
	}
}

func TestRoundWork(t *testing.T) {
	a, b := fixturegen.Addr(0), fixturegen.Addr(1)
	sh := map[int64][]poolfmt.Share{
		100: {addrShare(a, "w1", addrNow, 1000, 1, true, ""), addrShare(a, "w2", addrNow, 500, 1, true, ""),
			addrShare(a, "w1", addrNow, 9999, 1, false, "Stale"), addrShare(b, "w1", addrNow, 7777, 1, true, "")},
		101: {addrShare(a, "w1", addrNow, 250, 1, true, "")},
	}
	const nd = 1e6
	if got, want := RoundWork(sh, a, 100, nd), 1500.0/nd*100; got != want {
		t.Fatalf("height 100: got %v want %v", got, want)
	}
	if got, want := RoundWork(sh, a, 101, nd), 250.0/nd*100; got != want {
		t.Fatalf("a new height starts over: got %v want %v", got, want)
	}
	if RoundWork(sh, a, 102, nd) != 0 || RoundWork(sh, a, 100, 0) != 0 {
		t.Fatal("empty height or zero difficulty is zero")
	}
	// Identity is the username (or the worker name's address), never the IP.
	if RoundWork(sh, fixturegen.IP(9), 100, nd) != 0 {
		t.Fatal("the connection IP was credited")
	}
	noUser := map[int64][]poolfmt.Share{7: {{Diff: 40, Result: true, Address: fixturegen.IP(9),
		WorkerName: fixturegen.BareAddr(3) + ".rig01"}}}
	if got := RoundWork(noUser, fixturegen.Addr(3), 7, 100); got != 40 {
		t.Fatalf("worker-name fallback: %v", got)
	}
}

func addrCtx(p Provider, snap *Snapshot) *Context {
	caps := termcap.Detect(func(k string) string {
		switch k {
		case "LANG":
			return "en_US.UTF-8"
		case "TERM":
			return "xterm-256color"
		case "NO_COLOR":
			return "1"
		}
		return ""
	}, termcap.Flags{})
	return &Context{Snap: snap, Now: addrNow, Caps: caps, Theme: ThemeFor(caps), Provider: p, sym: symbolsFor(caps)}
}

func renderAddr(t *testing.T, p Provider, snap *Snapshot, addr string) string {
	t.Helper()
	lines := (&addressView{addr: addr}).Render(addrCtx(p, snap), 120, 34)
	for i, l := range lines {
		lines[i] = ansi.Strip(fit(l, 120))
	}
	for len(lines) < 34 {
		lines = append(lines, strings.Repeat(" ", 120))
	}
	return strings.Join(lines, "\n")
}

func addrSnap() *Snapshot {
	a := fixturegen.Addr(0)
	return &Snapshot{
		Pool: PoolCard{NetworkDiff: 1e12, Accepted: 2.5e11, Height: 900000},
		Workers: []WorkerRow{
			{Name: a + ".rig01", Address: a, UserAgent: "Miner/1.0", Hashrate1m: 2e12, Hashrate1h: 2e12, Diff: 65536, LastShare: addrNow.Add(-5 * time.Second)},
			{Name: a + ".rig02", Address: a, UserAgent: "Miner/1.0", Hashrate1m: 1e12, Hashrate1h: 1e12, Diff: 65536, LastShare: addrNow.Add(-40 * time.Minute)},
			{Name: fixturegen.Addr(1) + ".rig01", Address: fixturegen.Addr(1), Hashrate1m: 9e12, Diff: 1, LastShare: addrNow},
		},
	}
}

func TestAddressPageGolden(t *testing.T) {
	a := fixturegen.Addr(0)
	d := AddressData{Height: 900000, Work: 4e10, LogShares: true}
	for i := 0; i < 6; i++ {
		at := addrNow.Add(-time.Duration(i) * 7 * time.Second)
		d.Submits = append([]poolfmt.Share{addrShare(a, a+".rig01", at, 65536, 70000+float64(i), true, "")}, d.Submits...)
	}
	d.Submits = append(d.Submits, addrShare(a, a+".rig02", addrNow.Add(time.Second), 65536, 12, false, "Duplicate share"))
	golden.RequireEqual(t, []byte(renderAddr(t, addrProv{d}, addrSnap(), a)))
}

func TestAddressPageEmptyNoSharelogs(t *testing.T) {
	out := renderAddr(t, addrProv{}, addrSnap(), fixturegen.Addr(0))
	if !strings.Contains(out, "logshares") {
		t.Fatal("empty state must hint at logshares")
	}
	golden.RequireEqual(t, []byte(out))
}

func TestAddressPageWithoutSource(t *testing.T) {
	out := renderAddr(t, bareProv{}, addrSnap(), fixturegen.Addr(0))
	if !strings.Contains(out, "logshares") || !strings.Contains(out, "pool round since our last block") {
		t.Fatal("degraded page keeps the labels and the hint")
	}
}
