package ui

import (
	"math"
	"testing"
	"time"

	"cashstratumapi/cmd/cashtop/internal/termcap"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestPoolLuck(t *testing.T) {
	if got := PoolLuck(50, 200); got != 25 {
		t.Fatalf("PoolLuck = %v", got)
	}
	if PoolLuck(1, 0) != 0 || PoolLuck(-1, 10) != 0 {
		t.Fatal("degenerate inputs must give 0")
	}
	if got := UserLuck(300, 200); got != 150 {
		t.Fatalf("UserLuck = %v", got)
	}
}

func TestProbBlockWithin(t *testing.T) {
	d := 1e6
	h := d * two32 / 3600 // one expected block per hour
	if got := ProbBlockWithin(h, d, time.Hour); !near(got, 1-math.Exp(-1), 1e-12) {
		t.Fatalf("P(1h) = %v", got)
	}
	if got := ProbBlockWithin(h, d, 24*time.Hour); !near(got, 1-math.Exp(-24), 1e-12) {
		t.Fatalf("P(24h) = %v", got)
	}
	// Tiny rates keep precision instead of rounding to zero.
	if got := ProbBlockWithin(1, 1e12, time.Second); got <= 0 || !near(got, 1/(1e12*two32), 1e-30) {
		t.Fatalf("P(tiny) = %v", got)
	}
	if ProbBlockWithin(0, d, time.Hour) != 0 || ProbBlockWithin(h, 0, time.Hour) != 0 || ProbBlockWithin(h, d, 0) != 0 {
		t.Fatal("degenerate inputs must give 0")
	}
}

func TestETA(t *testing.T) {
	d := 1e6
	eta, ok := ETA(d*two32/600, d)
	if !ok || eta != 10*time.Minute {
		t.Fatalf("ETA = %v %v", eta, ok)
	}
	if _, ok := ETA(0, d); ok {
		t.Fatal("zero hashrate must not have an ETA")
	}
	if eta, ok := ETA(1e-9, 1e18); !ok || eta != time.Duration(math.MaxInt64) {
		t.Fatalf("overflow ETA = %v %v", eta, ok)
	}
}

func TestClassify(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	h := 1e12
	diff := h * 10 / two32 // one share every 10 s
	w := WorkerRow{Hashrate1m: h, Diff: diff}
	cases := []struct {
		last time.Duration
		want WorkerState
	}{{5 * time.Second, StateLive}, {59 * time.Second, StateLive}, {61 * time.Second, StateStale}}
	for _, c := range cases {
		w.LastShare = now.Add(-c.last)
		if got := Classify(w, now); got != c.want {
			t.Errorf("last %v: %v, want %v", c.last, got, c.want)
		}
	}
	slow := WorkerRow{Hashrate1m: h, Diff: diff * 60, LastShare: now.Add(-50 * time.Minute)} // 10 min interval
	if Classify(slow, now) != StateLive {
		t.Error("a slow worker within StaleK intervals is live")
	}
	if Classify(WorkerRow{LastShare: now}, now) != StateOff {
		t.Error("no hashrate is off")
	}
}

func TestPoolShareMatchesExpectedPerDay(t *testing.T) {
	const diff = 529e9
	for _, rate := range []float64{3.95e12, 1e12, 5e15} {
		share := poolShare(rate, diff)
		eta, ok := ETA(rate, diff)
		if !ok {
			t.Fatalf("no ETA for %v", rate)
		}
		perDay := float64(day) / float64(eta)
		if !near(share*144, perDay, perDay*1e-9) {
			t.Fatalf("rate %v: share x 144 = %v, expected/day = %v", rate, share*144, perDay)
		}
	}
	if poolShare(0, diff) != 0 || poolShare(1e12, 0) != 0 {
		t.Fatal("unknown inputs must give 0")
	}
	for f, want := range map[float64]string{0.017: "1.70%", 0.0001: "0.01%", 0.00001: "10.00 ppm", 0: dash} {
		if got := fmtPoolShare(f); got != want {
			t.Errorf("fmtPoolShare(%v) = %q, want %q", f, got, want)
		}
	}
}

func TestClassifyIdleVersusOff(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	idle := WorkerRow{Connected: now.Add(-time.Hour), LastShare: now.Add(-20 * time.Minute)}
	if got := Classify(idle, now); got != StateIdle {
		t.Errorf("connected with zero rates: %v, want idle", got)
	}
	if got := Classify(WorkerRow{LastShare: now.Add(-20 * time.Minute)}, now); got != StateOff {
		t.Errorf("not connected with zero rates: %v, want off", got)
	}
}

func TestLongHashDashesIdleAndOff(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	caps := termcap.Caps{}
	ctx := &Context{Snap: &Snapshot{}, Now: now, Caps: caps, Theme: ThemeFor(caps), sym: symbolsFor(caps)}
	idle := WorkerRow{Connected: now.Add(-time.Hour), Hashrate1h: 4}
	off := WorkerRow{Hashrate1h: 4}
	live := WorkerRow{Connected: now.Add(-time.Hour), Hashrate1m: 1e12, Hashrate1h: 4, LastShare: now}
	if got := ctx.longHash(idle, idle.Hashrate1h); got != dash {
		t.Errorf("idle 1h = %q", got)
	}
	if got := ctx.longHash(off, off.Hashrate1h); got != dash {
		t.Errorf("off 1h = %q", got)
	}
	if got := ctx.longHash(live, live.Hashrate1h); got == dash {
		t.Errorf("live 1h = %q", got)
	}
}

func TestWorkerStateOrdering(t *testing.T) {
	if !(StateLive < StateStale && StateStale < StateIdle && StateIdle < StateOff) {
		t.Fatal("state order must be live < stale < idle < off")
	}
}
