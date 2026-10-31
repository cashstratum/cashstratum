package demo

import (
	"reflect"
	"testing"
	"time"

	"cashstratumapi/cmd/cashtop/internal/ui"
)

var now = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func TestDeterministic(t *testing.T) {
	a := New(Options{Seed: 1, Now: now}).Snapshot()
	b := New(Options{Seed: 1, Now: now}).Snapshot()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed and time must give the same snapshot")
	}
	if len(a.Workers) == 0 || len(a.Blocks) == 0 || a.Pool.LastBlock.IsZero() {
		t.Fatal("default demo has workers and blocks")
	}
}

func TestScenarios(t *testing.T) {
	s := New(Options{Seed: 1, Now: now, NoBlocks: true, NoWorkers: true}).Snapshot()
	if len(s.Blocks) != 0 || len(s.Workers) != 0 || !s.Pool.LastBlock.IsZero() || s.Pool.Hashrate1m != 0 {
		t.Fatalf("empty scenario not empty: %+v", s.Pool)
	}
}

func TestDrillDownWinningShare(t *testing.T) {
	p := New(Options{Seed: 1, Now: now})
	s := p.Snapshot()
	h := s.Blocks[0].Height
	d, err := p.DrillDown(h)
	if err != nil || len(d.Jobs) != 3 {
		t.Fatalf("drill %v %v", d, err)
	}
	wins := 0
	for _, j := range d.Jobs {
		for _, sh := range j.Shares {
			if sh.Winning {
				wins++
				if sh.SDiff < d.NetworkDiff || sh.Worker != s.Blocks[0].Finder {
					t.Fatalf("bad winning share %+v", sh)
				}
			} else if sh.SDiff >= d.NetworkDiff {
				t.Fatalf("unflagged share above network difficulty %+v", sh)
			}
		}
	}
	if wins != 1 {
		t.Fatalf("wins = %d", wins)
	}
	if d, _ := p.DrillDown(s.Blocks[0].Height - 1); len(d.Jobs) == 0 {
		t.Fatal("a non-block height still has jobs")
	} else {
		for _, j := range d.Jobs {
			for _, sh := range j.Shares {
				if sh.Winning {
					t.Fatal("no winning share at a height the pool did not find")
				}
			}
		}
	}
}

func TestSharesAndAddress(t *testing.T) {
	p := New(Options{Seed: 1, Now: now})
	s := p.Snapshot()
	if len(s.Shares) == 0 || len(s.Shares) > 500 {
		t.Fatalf("shares = %d", len(s.Shares))
	}
	var near, rej int
	for i, d := range s.Shares {
		if i > 0 && d.Time.Before(s.Shares[i-1].Time) {
			t.Fatal("shares not oldest first")
		}
		if d.Near {
			near++
		}
		if !d.Accepted {
			rej++
		}
	}
	if near == 0 || rej == 0 {
		t.Fatalf("near=%d rejected=%d", near, rej)
	}
	a := p.Address(s.Workers[0].Address)
	if !a.LogShares || len(a.Submits) == 0 || a.Work <= 0 || a.Height != int64(s.Pool.Height) {
		t.Fatalf("address data %+v", a)
	}
	if e := New(Options{Seed: 1, Now: now, NoWorkers: true}); len(e.Snapshot().Shares) != 0 || e.Address(s.Workers[0].Address).LogShares {
		t.Fatal("empty scenario has shares")
	}
}

func TestBestSharesInvariants(t *testing.T) {
	s := New(Options{Seed: 1, Now: now}).Snapshot()
	if len(s.BestShares) == 0 {
		t.Fatal("no best shares")
	}
	for _, b := range s.BestShares {
		if !b.Time.After(s.Pool.LastBlock) {
			t.Fatalf("%+v is not after the last block %v", b, s.Pool.LastBlock)
		}
	}
	if top := s.BestShares[0].Diff; top > s.Pool.BestShare || top >= s.Pool.NetworkDiff {
		t.Fatalf("top %v vs pool best %v, network %v", top, s.Pool.BestShare, s.Pool.NetworkDiff)
	}
}

// Every drill-down share must report the agent and difficulty of its worker's
// row, and the winning share must belong to the block's finder.
func TestShowcaseDrillDownMatchesWorkers(t *testing.T) {
	p := Showcase(Options{Seed: 7, Now: now})
	s := p.Snapshot()
	rows := map[string]ui.WorkerRow{}
	for _, w := range s.Workers {
		rows[w.Name] = w
	}
	for _, b := range s.Blocks {
		d, err := p.DrillDown(b.Height)
		if err != nil || len(d.Jobs) == 0 {
			t.Fatalf("drill %d: %v", b.Height, err)
		}
		wins := 0
		for _, j := range d.Jobs {
			for _, sh := range j.Shares {
				w, ok := rows[sh.Worker]
				if !ok {
					t.Fatalf("height %d: share from unknown worker %q", b.Height, sh.Worker)
				}
				if sh.Agent != w.UserAgent || sh.Diff != w.Diff {
					t.Fatalf("height %d: share %s has agent %q diff %v, worker row has agent %q diff %v",
						b.Height, sh.Worker, sh.Agent, sh.Diff, w.UserAgent, w.Diff)
				}
				if sh.Winning {
					wins++
					if sh.Worker != b.Finder || sh.SDiff < d.NetworkDiff {
						t.Fatalf("height %d: bad winning share %+v, finder %s", b.Height, sh, b.Finder)
					}
				}
			}
		}
		if wins != 0 && wins != 1 {
			t.Fatalf("height %d: wins = %d", b.Height, wins)
		}
	}
}
