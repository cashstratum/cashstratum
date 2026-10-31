// Package demo provides a self-contained data source for running cashtop
// without pool files or a node: every value comes from the fixture
// generator, anchored at a fixed time.
package demo

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/poolfmt"
)

// Options select the demo scenario.
type Options struct {
	Seed      uint64
	Now       time.Time
	NoBlocks  bool // a pool that has not found a block yet
	NoWorkers bool // a pool with nobody connected
}

// Provider is a static ui.Provider built from fixturegen.
type Provider struct {
	opt    Options
	snap   ui.Snapshot
	shares []poolfmt.Share // current height's sharelog, oldest first
}

var agents = []string{
	"bmminer/2.0.0", "cgminer/4.12.1", "bitaxe/BM1370/v2.4.2", "NiceHash/1.0.0",
	"bosminer/24.05", "cpuminer/2.5.1",
}

const two32 = 4294967296.0

// New builds the demo snapshot once.
func New(o Options) *Provider {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	p := &Provider{opt: o}
	p.snap = build(o)
	if !o.NoWorkers {
		p.shares = shares(o, p.snap)
	}
	for i, sh := range p.shares {
		if i < len(p.shares)-ui.MaxShareDrops {
			continue
		}
		t, _ := shareTime(sh)
		p.snap.Shares = append(p.snap.Shares, ui.ShareDrop{Time: t, Worker: sh.WorkerName, Diff: sh.Diff, SDiff: sh.SDiff,
			Accepted: sh.Result, Near: sh.IsBlock(p.snap.Pool.NetworkDiff) || ui.IsNear(sh.SDiff, p.snap.Pool.NetworkDiff)})
	}
	return p
}

// Snapshot returns the same snapshot on every call.
func (p *Provider) Snapshot() ui.Snapshot { return p.snap }

func build(o Options) ui.Snapshot {
	d := fixturegen.DemoSnapshot(o.Seed, o.Now)
	now := o.Now
	dp := d.Pool
	s := ui.Snapshot{Taken: now, Hashrates: d.Hashrates}
	s.Pool = ui.PoolCard{
		Hashrate1m: dp.Hashrate1m, Hashrate5m: dp.Hashrate1m * 1.01, Hashrate15m: dp.Hashrate1m * 0.99,
		Hashrate1h: dp.Hashrate1d * 1.02, Hashrate6h: dp.Hashrate1d * 0.99, Hashrate1d: dp.Hashrate1d,
		Hashrate7d: dp.Hashrate1d * 0.96,
		Users:      dp.Users, Workers: dp.Workers, Idle: dp.Idle,
		SPS1m: dp.SPS1m, SPS5m: dp.SPS1m * 0.98,
		Accepted: float64(dp.Accepted), Rejected: float64(dp.Rejected),
		NetworkDiff: dp.NetworkDiff, RoundPercent: dp.Luck,
		Height: dp.Height, Runtime: dp.Runtime,
		AllAliveSince:      now.Add(-30 * time.Hour),
		LongestSession:     6*24*time.Hour + 4*time.Hour,
		LongestSessionName: fixturegen.Worker(0, 0),
	}
	s.Network = ui.NetworkInfo{
		Chain: "main", Tip: dp.Height - 1, TipHash: fixturegen.Hash(999), TipTime: now.Add(-4 * time.Minute),
		Difficulty: dp.NetworkDiff, NetHashrate: dp.NetworkDiff * two32 / 600,
		MempoolTx: 1840, MempoolBytes: 912_345, RPCLatency: 3 * time.Millisecond, RPCOK: true,
		ZMQLast: now.Add(-4 * time.Minute), TemplateTime: now.Add(-17 * time.Second), TemplateTxs: 1840,
	}

	if !o.NoWorkers {
		for i, w := range d.Workers {
			diff := 0.0
			if w.Hashrate1m > 0 {
				diff = math.Exp2(math.Round(math.Log2(w.Hashrate1m * 10 / two32)))
			}
			s.Workers = append(s.Workers, ui.WorkerRow{
				Name: w.Name, Address: w.Address, UserAgent: agents[i%len(agents)],
				Hashrate1m: w.Hashrate1m, Hashrate5m: w.Hashrate1m * 1.01, Hashrate1h: w.Hashrate1d * 1.01,
				Hashrate1d: w.Hashrate1d, Diff: diff,
				BestShare: dp.NetworkDiff * (0.0004 + 0.0021*float64((i*7)%11)),
				Shares:    w.Shares, LastShare: w.LastShare,
				Connected: now.Add(-time.Duration(20+i*9) * time.Hour),
			})
		}
		best := append([]ui.WorkerRow(nil), s.Workers...)
		sort.SliceStable(best, func(i, j int) bool { return best[i].BestShare > best[j].BestShare })
		for i, w := range best {
			if i == 8 {
				break
			}
			s.BestShares = append(s.BestShares, ui.BestShare{
				Worker: w.Name, Diff: w.BestShare, Time: now.Add(-time.Duration(13+i*47) * time.Minute),
			})
		}
		s.Pool.BestShare = best[0].BestShare
	} else {
		s.Pool.Hashrate1m, s.Pool.Hashrate5m, s.Pool.Hashrate15m = 0, 0, 0
		s.Pool.Hashrate1h, s.Pool.Hashrate6h, s.Pool.Hashrate1d, s.Pool.Hashrate7d = 0, 0, 0, 0
		s.Pool.Users, s.Pool.Workers, s.Pool.Idle = 0, 0, 0
		s.Pool.SPS1m, s.Pool.SPS5m, s.Pool.Accepted, s.Pool.Rejected, s.Pool.RoundPercent = 0, 0, 0, 0, 0
		s.Pool.AllAliveSince, s.Pool.LongestSession, s.Pool.LongestSessionName = time.Time{}, 0, ""
		s.Hashrates = nil
	}

	if !o.NoBlocks {
		// The fixture's heights are shifted so the newest find is 36 blocks
		// below the tip; times follow from the ten-minute block target, so
		// confirmations, heights and ages all agree.
		const target = 10 * time.Minute
		for i, b := range d.Blocks {
			proto := "SV1"
			if i == 1 {
				proto = "SV2"
			}
			h := b.Height - 36
			conf := s.Network.Tip - h + 1
			s.Blocks = append(s.Blocks, ui.BlockRow{
				Height: h, Hash: b.Hash, Finder: b.Finder, Proto: proto,
				Time: s.Network.TipTime.Add(-time.Duration(conf-1) * target), Luck: b.Luck,
				Status: ui.BlockConfirmed, Confirmations: conf,
				Reward: 312_500_000 + int64(41_000+i*13_700),
			})
		}
		s.Pool.LastBlock = s.Blocks[0].Time
	}
	s.Feed = feed(s, now)
	return s
}

func feed(s ui.Snapshot, now time.Time) []ui.FeedRow {
	var rows []ui.FeedRow
	add := func(ago time.Duration, style, text string, colour int) {
		rows = append(rows, ui.FeedRow{Time: now.Add(-ago), Style: style, Text: text, Colour: colour})
	}
	add(58*time.Minute, "NET", fmt.Sprintf("new network block %d", s.Network.Tip-3), 6)
	for i, w := range s.Workers {
		if i%3 == 0 {
			add(time.Duration(55-i)*time.Minute, "AUTH", fmt.Sprintf("%s authorised (%s)", w.Name, w.UserAgent), 1)
		}
	}
	add(41*time.Minute, "NET", fmt.Sprintf("new network block %d", s.Network.Tip-2), 6)
	if len(s.Workers) > 0 {
		add(33*time.Minute, "DROP", s.Workers[len(s.Workers)-1].Name+" dropped: idle", 2)
	}
	add(22*time.Minute, "NET", fmt.Sprintf("new network block %d", s.Network.Tip-1), 6)
	if len(s.Blocks) > 0 {
		b := s.Blocks[0]
		add(now.Sub(b.Time), "BLOCK", fmt.Sprintf("block %d solved by %s", b.Height, b.Finder), 4)
	}
	add(4*time.Minute, "NET", fmt.Sprintf("new network block %d", s.Network.Tip), 6)
	add(time.Minute, "POOL", fmt.Sprintf("hashrate %s 1m, %d workers, %d idle",
		fixturegen.SuffixString(s.Pool.Hashrate1m)+"H/s", s.Pool.Workers, s.Pool.Idle), 0)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Time.Before(rows[j].Time) })
	return rows
}

// DrillDown synthesises the sharelog of a height: three jobs, and for a
// height the pool found, a winning share in the last job.
func (p *Provider) DrillDown(height int) (ui.DrillDown, error) {
	s := p.snap
	dd := ui.DrillDown{Height: height, Dir: fmt.Sprintf("%08x", height), NetworkDiff: s.Pool.NetworkDiff}
	if height <= 0 || height > s.Pool.Height || len(s.Workers) == 0 {
		return dd, nil
	}
	var found *ui.BlockRow
	for i := range s.Blocks {
		if s.Blocks[i].Height == height {
			found = &s.Blocks[i]
		}
	}
	base := p.opt.Now.Add(-10 * time.Minute)
	if found != nil {
		base = found.Time.Add(-9 * time.Minute)
	}
	n := 0
	for j := 0; j < 3; j++ {
		job := ui.DrillJob{ID: fmt.Sprintf("%x", height*16+j)}
		for k := 0; k < 4; k++ {
			w := s.Workers[(j*4+k)%len(s.Workers)]
			if w.Diff == 0 {
				w = s.Workers[0]
			}
			sh := ui.DrillShare{
				Time: base.Add(time.Duration(n) * 41 * time.Second), Worker: w.Name, Agent: w.UserAgent,
				Diff: w.Diff, SDiff: w.Diff * float64(1+(n*5)%7), Result: true,
			}
			if n == 6 {
				sh.Result, sh.Error = false, "Stale"
			}
			job.Shares = append(job.Shares, sh)
			n++
		}
		dd.Jobs = append(dd.Jobs, job)
	}
	if found != nil {
		// The winning share belongs to the finder, so it carries the finder's
		// agent and difficulty, not those of the slot it replaces.
		last := &dd.Jobs[2].Shares[3]
		last.Worker, last.Time = found.Finder, found.Time
		for _, w := range s.Workers {
			if w.Name == found.Finder {
				last.Agent, last.Diff = w.UserAgent, w.Diff
			}
		}
		last.SDiff = s.Pool.NetworkDiff * (1 + found.Luck/1000)
		last.Winning = last.SDiff >= s.Pool.NetworkDiff
	}
	return dd, nil
}

// shares renders ten minutes of submits for the current height through the
// fixture generator and parses them back like a real sharelog.
func shares(o Options, s ui.Snapshot) []poolfmt.Share {
	g := fixturegen.New(o.Seed+11, o.Now.Add(-10*time.Minute))
	nd := s.Pool.NetworkDiff
	diffs := []float64{512, 1024, 2048}
	var out []poolfmt.Share
	for i := 0; i < 300; i++ {
		g.Advance(2 * time.Second)
		d := diffs[i%len(diffs)]
		sd := d * float64(1+(i*7)%9)
		if i%97 == 41 {
			sd = nd * 0.02 // an occasional near miss
		}
		line := g.ShareLine(int64(s.Pool.Height)*16+int64(i/100), i%23 != 5, d, sd)
		if sh, err := poolfmt.ParseSharelogLine(line); err == nil {
			// Report the same useragent as the matching worker row.
			_, rig := poolfmt.SplitWorker(sh.WorkerName)
			for _, w := range s.Workers {
				if sameAddr(w.Address, sh.UserName) && strings.HasSuffix(w.Name, "."+rig) {
					sh.Agent = w.UserAgent
				}
			}
			out = append(out, sh)
		}
	}
	return out
}

func shareTime(s poolfmt.Share) (time.Time, bool) {
	sec, _, _ := strings.Cut(s.CreateDate, ",")
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, false
	}
	return time.Unix(n, 0).UTC(), true
}

// payout is the share's payout address: the username, or the address part
// of the worker name. The sharelog's "address" field is the client's IP.
func payout(sh poolfmt.Share) string {
	if sh.UserName != "" {
		return sh.UserName
	}
	a, _ := poolfmt.SplitWorker(sh.WorkerName)
	return a
}

func sameAddr(a, b string) bool {
	return strings.TrimPrefix(a, fixturegen.CashAddrPrefix+":") == strings.TrimPrefix(b, fixturegen.CashAddrPrefix+":")
}

// Address implements ui.AddressSource.
func (p *Provider) Address(addr string) ui.AddressData {
	d := ui.AddressData{Height: int64(p.snap.Pool.Height), LogShares: len(p.shares) > 0, LastShare: map[string]time.Time{}}
	for _, sh := range p.shares {
		if !sameAddr(payout(sh), addr) {
			continue
		}
		if sh.Result {
			d.Work += sh.Diff
		}
		d.Submits = append(d.Submits, sh)
	}
	if n := len(d.Submits); n > 50 {
		d.Submits = d.Submits[n-50:]
	}
	for _, w := range p.snap.Workers {
		if sameAddr(w.Address, addr) && !w.LastShare.IsZero() {
			d.LastShare[w.Name] = w.LastShare
		}
	}
	return d
}
