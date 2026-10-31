package fixturegen

import (
	"fmt"
	"time"
)

// BlockEvent describes one block the simulated pool found.
type BlockEvent struct {
	Height int
	Hash   string
	Finder string
	Stamp  string
	Window []string
}

// Sim is the result of Simulate.
type Sim struct {
	Lines    []string            // time-ordered, prefixed log lines
	Shares   map[string][]string // SharelogPath -> compact JSON lines
	Found    []BlockEvent        // accepted blocks
	Rejected []int               // heights of rejected submits
}

// Simulate runs the pool for the given number of one-minute cycles. A block is
// found every ten cycles starting at cycle 5, a rejected submit at cycle 12,
// and a foreign tip at cycle 8 and then every 17th cycle.
func (g *Generator) Simulate(minutes int) *Sim {
	s := &Sim{Shares: map[string][]string{}}
	emit := func(ls ...string) { s.Lines = append(s.Lines, g.splitLines(ls)...) }
	wid := int64(7_700_000_000_000_000_000)
	for m := 0; m < minutes; m++ {
		g.Advance(60*time.Second + 3*time.Millisecond)
		emit(g.Tick()...)
		for i := 0; i < 2; i++ {
			g.Advance(time.Duration(5+g.rng.IntN(5000)) * time.Millisecond)
			wid += int64(1 + g.rng.IntN(1000))
			emit(g.Workbase())
			path := SharelogPath(g.Height, wid)
			for k := 0; k < 6; k++ {
				s.Shares[path] = append(s.Shares[path], g.ShareLine(wid, k != 4, 5000, 5000+float64(g.rng.IntN(100000))))
			}
		}
		g.Advance(time.Duration(g.rng.IntN(800)) * time.Millisecond)
		emit(g.Authorised(), g.Dropped(true, false), g.Dropped(false, false), g.Dropped(true, true))
		if m == 1 {
			emit(g.Extra()...)
		}
		switch {
		case m%10 == 5:
			u, w := g.rng.IntN(g.nUsers), g.rng.IntN(g.nWorkers)
			finder := FullWorker(u, w)
			h := g.Height
			stamp := g.now.Format("2006-01-02 15:04:05")
			path := SharelogPath(h, wid)
			s.Shares[path] = append(s.Shares[path], g.ShareLine(wid, true, 5000, g.NetworkDiff*1.05))
			ls := g.BlockCluster(h, finder, "SV1")
			emit(ls...)
			s.Found = append(s.Found, BlockEvent{Height: h, Hash: Hash(g.nextHash - 1), Finder: finder, Stamp: stamp, Window: ls})
		case m%10 == 2 && m > 2:
			s.Rejected = append(s.Rejected, g.Height)
			emit(g.RejectedCluster(g.Height, "high-hash")...)
		case m == 8 || (m > 8 && m%17 == 0):
			emit(g.NewTip()...)
		}
	}
	return s
}

func (e BlockEvent) String() string { return fmt.Sprintf("%d %s %s", e.Height, e.Hash, e.Finder) }
