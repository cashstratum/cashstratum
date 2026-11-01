package demo

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"time"

	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/poolfmt"
)

// ShowcaseNow is the clock the showcase profile is anchored at when the
// caller does not supply one.
var ShowcaseNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// ShowcaseHost is the only pool name the showcase ever displays.
const ShowcaseHost = "pool.example.com"

type showRig struct {
	user   int
	name   string
	agent  string
	rate   float64 // H/s
	state  byte    // 'l' live, 's' stale, 'i' idle, 'o' off
	lastAs time.Duration
}

// showRound is the current round effort, in percent of network difficulty.
const showRound = 68.0

const (
	gh = 1e9
	th = 1e12
	ph = 1e15
)

var showRigs = []showRig{
	{0, "s21-a", "bmminer/2.0.0", 1.28 * ph, 'l', 4 * time.Second},
	{0, "s19-b", "cgminer/4.12.1", 0.61 * ph, 'l', 7 * time.Second},
	{1, "m60-a", "btminer/2.0.1", 0.96 * ph, 'l', 3 * time.Second},
	{1, "m50-b", "btminer/2.0.1", 0.42 * ph, 'l', 11 * time.Second},
	{2, "s21-c", "bmminer/2.0.0", 0.87 * ph, 'l', 6 * time.Second},
	{2, "s19-d", "cgminer/4.12.1", 0.36 * ph, 's', 7 * time.Minute},
	{3, "nh-rental", "NiceHash/1.0.0", 0.52 * ph, 'l', 9 * time.Second},
	{3, "bitaxe-1", "bitaxe/BM1370/v2.4.2", 1.18 * th, 'l', 14 * time.Second},
	{3, "nerdq-1", "NerdQAxe++/v1.0.3", 4.9 * th, 'l', 12 * time.Second},
	{4, "bitaxe-2", "bitaxe/BM1366/v2.3.0", 0.93 * th, 's', 5 * time.Minute},
	{4, "nerdq-2", "NerdQAxe++/v1.0.3", 0, 'o', 26 * time.Hour},
	{2, "s19-f", "cgminer/4.12.1", 0.31 * ph, 'i', 19 * time.Minute},
	{4, "s19-e", "cgminer/4.12.1", 0.29 * ph, 'l', 8 * time.Second},
}

// Efforts of the 30 blocks, newest first, in percent of network difficulty:
// a spread from a lucky 3% to an unlucky 330%.
var showEfforts = [30]float64{
	58.2, 141.7, 22.4, 96.1, 330.0, 74.8, 3.1, 187.5, 41.3, 119.9,
	12.6, 268.4, 83.0, 52.7, 161.2, 9.8, 105.3, 37.9, 214.6, 66.4,
	28.5, 147.0, 7.4, 92.8, 175.9, 49.1, 134.6, 18.2, 78.5, 101.7,
}

// Hours between consecutive finds, newest first; the first entry is replaced
// by the age that puts the current round at showRound.
var showGapHours = [30]float64{
	3.4, 31.0, 12.5, 40.8, 6.3, 19.7, 30.4, 8.9, 27.6, 14.2,
	35.1, 5.8, 22.9, 26.0, 10.7, 17.3, 29.4, 2.6, 38.5, 13.1,
	24.8, 9.4, 27.2, 16.0, 7.9, 33.3, 21.5, 11.2, 36.7, 20.0,
}

// Showcase builds a healthy, busy solo pool with synthetic values only. The
// result is deterministic for a given seed and clock.
func Showcase(o Options) *Provider {
	if o.Now.IsZero() {
		o.Now = ShowcaseNow
	}
	if o.Seed == 0 {
		o.Seed = 7
	}
	rng := rand.New(rand.NewPCG(o.Seed, 0x5c0ffee))
	now := o.Now
	p := &Provider{opt: o}
	s := ui.Snapshot{Taken: now}

	// Network.
	const tip = 951_204
	netDiff := 530.4 * gh
	tipTime := now.Add(-4*time.Minute - 12*time.Second)
	s.Network = ui.NetworkInfo{
		Chain: "main", Tip: tip, TipHash: fixturegen.Hash(9001), TipTime: tipTime,
		Difficulty: netDiff, NetHashrate: netDiff * two32 / 600,
		MempoolTx: 3412, MempoolBytes: 1_284_906, RPCLatency: 4 * time.Millisecond, RPCOK: true,
		ZMQLast: tipTime, ZMQConfigured: true, ZMQConnected: true, TemplateTime: now.Add(-23 * time.Second), TemplateTxs: 3388,
		LastNetBlock: tipTime, PrevDifficulty: netDiff / 1.0042, PrevTipTime: tipTime.Add(-11*time.Minute - 38*time.Second),
	}

	// Workers.
	var total, sps float64
	oldest := time.Duration(0)
	oldestName := ""
	for i, r := range showRigs {
		name := fmt.Sprintf("user%d.%s", r.user+1, r.name)
		w := ui.WorkerRow{Name: name, Address: fixturegen.Addr(r.user), UserAgent: r.agent}
		if r.state != 'o' && r.state != 'i' {
			jit := func(spread float64) float64 { return 1 + spread*(rng.Float64()*2-1) }
			w.Hashrate1m = r.rate * jit(0.03)
			w.Hashrate5m = r.rate * jit(0.02)
			w.Hashrate1h = r.rate * jit(0.025)
			w.Hashrate1d = r.rate * jit(0.03)
			w.Diff = math.Exp2(math.Round(math.Log2(w.Hashrate1m * 10 / two32)))
			if w.Diff < 1 {
				w.Diff = 1
			}
			total += w.Hashrate1m
			if r.state == 'l' {
				sps += w.Hashrate1m / (w.Diff * two32)
			}
		}
		w.Shares = int64(r.rate/two32*3600*float64(30+i*11)) + int64(rng.IntN(5000))
		if r.state == 'o' {
			w.Shares = 38_412
			w.Hashrate1h = 4.0
		}
		if r.state == 'i' {
			w.Hashrate1h = r.rate * 0.4
			w.Hashrate1d = r.rate * 0.7
		}
		w.LastShare = now.Add(-r.lastAs)
		conn := time.Duration(9+i*53) * time.Hour
		if r.state != 'o' {
			w.Connected = now.Add(-conn)
		}
		if conn > oldest && r.state != 'o' {
			oldest, oldestName = conn, w.Name
		}
		s.Workers = append(s.Workers, w)
	}

	// Blocks. The newest one is as old as the pool needs to accumulate the
	// current round at its average hashrate.
	avg := total * 0.985
	roundAge := time.Duration(showRound / 100 * netDiff * two32 / avg * float64(time.Second)).Round(time.Minute)
	age := time.Duration(0)
	for i := range showEfforts {
		if i == 0 {
			age = roundAge
		} else {
			age += time.Duration(showGapHours[i] * float64(time.Hour))
		}
		bt := now.Add(-age)
		conf := int(tipTime.Sub(bt)/(10*time.Minute)) + 1
		status := ui.BlockConfirmed
		if conf < 100 {
			status = ui.BlockPending
		}
		proto := "SV1"
		if i%9 == 4 {
			proto = "SV2"
		}
		finder := s.Workers[[]int{0, 2, 4, 1, 6, 3, 11, 0, 2, 8}[i%10]].Name
		s.Blocks = append(s.Blocks, ui.BlockRow{
			Height: tip - conf + 1, Hash: fixturegen.Hash(100 + i), Finder: finder, Proto: proto,
			Time: bt, Luck: showEfforts[i], Status: status, Confirmations: conf,
			Reward: 312_500_000 + int64(18_000+rng.IntN(380_000)),
		})
	}
	lastBlock := s.Blocks[0].Time

	// Pool card.
	since := now.Sub(lastBlock).Seconds()
	accepted := avg * since / two32
	s.Pool = ui.PoolCard{
		Hashrate1m: total, Hashrate5m: total * 0.987, Hashrate15m: total * 1.011, Hashrate1h: total * 0.972,
		Hashrate6h: total * 0.958, Hashrate1d: total * 0.981, Hashrate7d: total * 0.934,
		Users: 5, Workers: len(showRigs), Idle: 1, Disconnected: 3,
		SPS1m: sps, SPS5m: sps * 0.99,
		Accepted: accepted, Rejected: accepted * 0.0061, NetworkDiff: netDiff, RoundPercent: accepted / netDiff * 100,
		Height: tip + 1, LastBlock: lastBlock, Runtime: 41*24*time.Hour + 6*time.Hour,
		AllAliveSince: now.Add(-26 * time.Hour), LongestSession: oldest, LongestSessionName: oldestName,
	}

	// Hashrate series: slow drift plus noise, ending on the 1m figure.
	level := 1.0
	for i := 0; i < 60; i++ {
		level += (1-level)*0.18 + 0.022*(rng.Float64()*2-1)
		v := avg * level * (1 + 0.012*math.Sin(float64(i)/4.5))
		s.Hashrates = append(s.Hashrates, v)
	}
	s.Hashrates[59] = total

	// Best shares since the last block; the top one stays below the network
	// difficulty.
	frac := []float64{0.0471, 0.0312, 0.0224, 0.0158, 0.0121, 0.0094, 0.0071, 0.0052}
	who := []int{2, 0, 4, 6, 1, 8, 11, 3}
	mins := []int{6, 8, 3, 14, 22, 31, 39, 47}
	for i, f := range frac {
		wi := who[i]
		s.Workers[wi].BestShare = netDiff * f
		s.BestShares = append(s.BestShares, ui.BestShare{
			Worker: s.Workers[wi].Name, Diff: netDiff * f, Time: now.Add(-time.Duration(mins[i]) * time.Minute),
		})
	}
	for i := range s.Workers {
		if s.Workers[i].BestShare == 0 && s.Workers[i].Hashrate1m > 0 {
			s.Workers[i].BestShare = s.Workers[i].Diff * float64(400+rng.IntN(900))
		}
	}
	s.Pool.BestShare = s.BestShares[0].Diff

	s.Feed = showFeed(s, now, rng)
	p.snap = s
	p.shares = showShares(s, now, rng)
	for _, sh := range p.shares {
		t, _ := shareTime(sh)
		p.snap.Shares = append(p.snap.Shares, ui.ShareDrop{Time: t, Worker: sh.WorkerName, Diff: sh.Diff, SDiff: sh.SDiff,
			Accepted: sh.Result, Near: ui.IsNear(sh.SDiff, netDiff)})
	}
	if n := len(p.snap.Shares); n > ui.MaxShareDrops {
		p.snap.Shares = p.snap.Shares[n-ui.MaxShareDrops:]
	}
	return p
}

func showFeed(s ui.Snapshot, now time.Time, rng *rand.Rand) []ui.FeedRow {
	var rows []ui.FeedRow
	add := func(ago time.Duration, style, text string, colour int) {
		rows = append(rows, ui.FeedRow{Time: now.Add(-ago), Style: style, Text: text, Colour: colour})
	}
	// Network blocks about every ten minutes, the newest 4m12s ago.
	ago := 4*time.Minute + 12*time.Second
	for i := 0; ago < 95*time.Minute; i++ {
		add(ago, "NET", fmt.Sprintf("new network block %d", s.Network.Tip-i), 6)
		ago += time.Duration(330+rng.IntN(600)) * time.Second
	}
	for i, w := range s.Workers {
		if w.Hashrate1m <= 0 {
			continue
		}
		off := time.Duration(6+i*7) * time.Minute
		add(off, "CONN", fmt.Sprintf("client %d %s connected", 140+i, fixturegen.IP(i*5+2)), 5)
		add(off-time.Second, "AUTH", fmt.Sprintf("%s authorised (%s)", w.Name, w.UserAgent), 1)
	}
	add(33*time.Minute, "DROP", s.Workers[9].Name+" dropped: peer_reset", 2)
	add(33*time.Minute-40*time.Second, "CONN", fmt.Sprintf("client 151 %s connected", fixturegen.IP(61)), 5)
	add(33*time.Minute-41*time.Second, "AUTH", s.Workers[9].Name+" authorised ("+s.Workers[9].UserAgent+")", 1)
	add(58*time.Minute, "DROP", s.Workers[5].Name+" dropped: idle_drop", 2)
	add(71*time.Minute, "DROP", s.Workers[10].Name+" dropped: epollhup", 2)
	add(now.Sub(s.BestShares[0].Time), "BEST", fmt.Sprintf("%s best share %s", s.BestShares[0].Worker, fixturegen.SuffixString(s.BestShares[0].Diff)), 4)
	b := s.Blocks[0]
	add(now.Sub(b.Time), "BLOCK", fmt.Sprintf("block %d solved by %s", b.Height, b.Finder), 4)
	for m := 0; m <= 90; m += 15 {
		hr := s.Pool.Hashrate1m * (1 + 0.012*math.Sin(float64(m)/9))
		add(time.Duration(m)*time.Minute+20*time.Second, "POOL",
			fmt.Sprintf("hashrate %sH/s 1m, %d workers, %d idle", fixturegen.SuffixString(hr), s.Pool.Workers, s.Pool.Idle), 0)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Time.Before(rows[j].Time) })
	return rows
}

// showShares renders the last ten minutes of submits, including the near
// shares that appear in the best-share list.
func showShares(s ui.Snapshot, now time.Time, rng *rand.Rand) []poolfmt.Share {
	var out []poolfmt.Share
	mk := func(t time.Time, wi int, diff, sdiff float64, ok bool) poolfmt.Share {
		w := s.Workers[wi]
		sh := poolfmt.Share{
			Diff: diff, SDiff: sdiff, Result: ok, Agent: w.UserAgent, WorkerName: w.Name, UserName: w.Address,
			CreateDate: strconv.FormatInt(t.Unix(), 10) + ",0", CreateInet: fixturegen.IP(wi*5 + 2),
			Address: fixturegen.IP(wi*5 + 2),
		}
		if !ok {
			sh.Error, sh.Errn = "Stale", 21
		}
		return sh
	}
	var live []int
	for i, w := range s.Workers {
		if workerState(w, now) == 'l' {
			live = append(live, i)
		}
	}
	const window = 10 * time.Minute
	for i := 0; i < 420; i++ {
		t := now.Add(-window + time.Duration(i)*window/420)
		wi := live[rng.IntN(len(live))]
		w := s.Workers[wi]
		sd := w.Diff * (1 + rng.ExpFloat64()*1.4)
		out = append(out, mk(t, wi, w.Diff, sd, rng.IntN(100) >= 3))
	}
	// Near shares from the leaderboard that fall inside the window.
	idx := map[string]int{}
	for i, w := range s.Workers {
		idx[w.Name] = i
	}
	for _, b := range s.BestShares {
		if now.Sub(b.Time) < window && b.Diff >= ui.NearFraction*s.Pool.NetworkDiff {
			wi := idx[b.Worker]
			out = append(out, mk(b.Time, wi, s.Workers[wi].Diff, b.Diff, true))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := shareTime(out[i])
		b, _ := shareTime(out[j])
		return a.Before(b)
	})
	return out
}

// workerState mirrors the worker state letters used by the showcase table: 'l'
// live, 's' stale, 'i' idle, 'o' off.
func workerState(w ui.WorkerRow, now time.Time) byte {
	switch ui.Classify(w, now) {
	case ui.StateLive:
		return 'l'
	case ui.StateStale:
		return 's'
	case ui.StateIdle:
		return 'i'
	}
	return 'o'
}
