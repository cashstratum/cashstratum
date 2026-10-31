package fixturegen

import "time"

// DemoWorker is one row of the demo worker table.
type DemoWorker struct {
	Name       string
	Address    string
	Hashrate1m float64
	Hashrate1d float64
	Shares     int64
	LastShare  time.Time
	Online     bool
}

// DemoBlock is one entry of the demo block history.
type DemoBlock struct {
	Height int
	Hash   string
	Finder string
	Found  time.Time
	Luck   float64 // percent of network difficulty at the find
}

// DemoPool carries the headline pool statistics.
type DemoPool struct {
	Runtime     time.Duration
	Users       int
	Workers     int
	Idle        int
	Hashrate1m  float64
	Hashrate1d  float64
	NetworkDiff float64
	Luck        float64
	Height      int
	SPS1m       float64
	Accepted    int64
	Rejected    int64
}

// Demo is a self-contained snapshot for running without files or network.
type Demo struct {
	Now       time.Time
	Pool      DemoPool
	Workers   []DemoWorker
	Blocks    []DemoBlock
	Hashrates []float64 // trailing series, oldest first, one point per minute
}

// DemoSnapshot builds a deterministic snapshot anchored at now.
func DemoSnapshot(seed uint64, now time.Time) Demo {
	g := New(seed, now.Add(-9*24*time.Hour))
	g.Advance(9 * 24 * time.Hour)
	g.round = 41.7
	d := Demo{Now: now}
	var total float64
	for u := 0; u < 4; u++ {
		for w := 0; w < 3; w++ {
			r := g.rate(u) / 3
			online := !(u == 3 && w == 2)
			last := now.Add(-time.Duration(1+g.rng.IntN(25)) * time.Second)
			if !online {
				r, last = 0, now.Add(-30*time.Hour)
			}
			total += r
			d.Workers = append(d.Workers, DemoWorker{
				Name: Worker(u, w), Address: Addr(u), Hashrate1m: r, Hashrate1d: r * 0.97,
				Shares: 200_000 + int64(g.rng.IntN(900_000)), LastShare: last, Online: online,
			})
		}
	}
	for i := 0; i < 5; i++ {
		d.Blocks = append(d.Blocks, DemoBlock{
			Height: g.Height - 1 - i*37, Hash: Hash(100 + i), Finder: FullWorker(i%4, i%3),
			Found: now.Add(-time.Duration(6+i*41) * time.Hour), Luck: 20 + 180*g.rng.Float64(),
		})
	}
	for i := 0; i < 60; i++ {
		d.Hashrates = append(d.Hashrates, total*(0.85+0.3*g.rng.Float64()))
	}
	d.Pool = DemoPool{
		Runtime: 9 * 24 * time.Hour, Users: 4, Workers: 11, Idle: 1, Hashrate1m: total, Hashrate1d: total * 0.98,
		NetworkDiff: g.NetworkDiff, Luck: g.round, Height: g.Height, SPS1m: 123.4,
		Accepted: int64(g.NetworkDiff * g.round / 100), Rejected: 4_200_000,
	}
	return d
}
