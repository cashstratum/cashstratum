package app

import (
	"sort"
	"time"

	"cashstratumapi/cmd/cashtop/internal/index"
	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/poolfmt"
)

// networkDiff prefers the node, then the log, then the users files, then the
// pool's own round figure (accepted x 100 / percent).
func (a *App) networkDiff() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.networkDiffLocked()
}

func (a *App) networkDiffLocked() float64 {
	if a.net.RPCOK && a.net.Difficulty > 0 {
		return a.net.Difficulty
	}
	if a.logNet.diff > 0 {
		return a.logNet.diff
	}
	for _, u := range a.stats.users {
		if u.NetworkDiff.OK && u.NetworkDiff.V > 0 {
			return u.NetworkDiff.V
		}
	}
	if p := a.stats.pool; p.Diff > 0 && p.Accepted > 0 {
		return float64(p.Accepted) * 100 / p.Diff
	}
	return 0
}

func unix(o poolfmt.Opt[int64]) time.Time {
	if !o.OK || o.V <= 0 {
		return time.Time{}
	}
	return time.Unix(o.V, 0).UTC()
}

func (a *App) snapshot(withFeed bool) ui.Snapshot {
	v := a.ix.Snapshot()
	now := a.cfg.Now()
	lv := a.liveness(now)
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stats
	nd := a.networkDiffLocked()
	s := ui.Snapshot{Taken: now}

	p := st.pool
	s.Pool = ui.PoolCard{
		Hashrate1m: p.Hashrate1m, Hashrate5m: p.Hashrate5m, Hashrate15m: p.Hashrate15m, Hashrate1h: p.Hashrate1hr,
		Hashrate6h: p.Hashrate6hr, Hashrate1d: p.Hashrate1d, Hashrate7d: p.Hashrate7d,
		Users: int(p.Users), Workers: int(p.Workers), Idle: int(p.Idle), Disconnected: int(p.Disconnected),
		SPS1m: p.SPS1m, SPS5m: p.SPS5m,
		Accepted: float64(p.Accepted), Rejected: float64(p.Rejected), NetworkDiff: nd,
		RoundPercent: p.Diff, BestShare: p.BestShare,
		Runtime: time.Duration(p.Runtime) * time.Second,
	}

	s.Pool.Stale, s.Pool.LastActivity, s.Pool.StaleReason = lv.Stale, lv.Since, lv.Reason

	// Workers from users/*, with agent, difficulty and sub-minute liveness
	// from the current sharelogs.
	online := map[string]index.Worker{}
	for _, w := range v.Workers {
		online[w.Name] = w
	}
	addrs := make([]string, 0, len(st.users))
	for a := range st.users {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	allLive := len(addrs) > 0
	var lastDrop time.Time
	for _, addr := range addrs {
		u := st.users[addr]
		for _, w := range u.Worker {
			row := ui.WorkerRow{
				Name: w.Name, Address: addr, UserAgent: st.agents[w.Name],
				Hashrate1m: w.Hashrate1m, Hashrate5m: w.Hashrate5m, Hashrate1h: w.Hashrate1hr, Hashrate1d: w.Hashrate1d,
				Diff: st.diffs[w.Name], Shares: w.Shares.V, LastShare: unix(w.LastShare),
			}
			if w.BestShare.OK {
				row.BestShare = w.BestShare.V
			}
			if t := st.lastShare[w.Name]; t.After(row.LastShare) {
				row.LastShare = t
			}
			iw, ok := online[w.Name]
			if !ok {
				iw, ok = online[poolfmt.NormalizeAddress(addr)+"."+rigOf(w.Name)]
			}
			if ok && iw.Online {
				row.Connected = iw.Since
			}
			if ok && iw.LastDrop.After(lastDrop) {
				lastDrop = iw.LastDrop
			}
			if ui.Classify(row, now) != ui.StateLive {
				allLive = false
			}
			s.Workers = append(s.Workers, row)
		}
	}
	if allLive && len(s.Workers) > 0 && !lv.Stale {
		s.Pool.AllAliveSince = now.Add(-s.Pool.Runtime)
		if lastDrop.After(s.Pool.AllAliveSince) {
			s.Pool.AllAliveSince = lastDrop
		}
	}
	for _, w := range v.Workers {
		d := w.Streak(now)
		if rt := s.Pool.Runtime; rt > 0 && d > rt {
			d = rt // no session outlives the pool process
		}
		if d > s.Pool.LongestSession {
			s.Pool.LongestSession, s.Pool.LongestSessionName = d, w.Name
		}
	}

	tip := a.net.Tip
	if tip == 0 {
		tip = int(v.Tip) // last height the node reported before this start
	}
	for _, b := range v.Blocks {
		r := ui.BlockRow{Height: int(b.Height), Hash: b.Hash, Finder: b.Finder, Proto: protoLabel(b.Proto),
			Time: b.Time, Reward: max(b.Reward, a.rewards[b.Height])}
		if b.Luck != nil {
			r.Luck = *b.Luck
		}
		switch b.Status {
		case index.StatusRejected:
			r.Status = ui.BlockRejected
		case index.StatusOrphaned:
			r.Status = ui.BlockOrphaned
		default:
			r.Status = ui.BlockPending
			if tip >= int(b.Height) {
				r.Status, r.Confirmations = ui.BlockConfirmed, tip-int(b.Height)+1
			}
		}
		s.Blocks = append(s.Blocks, r)
		if s.Pool.LastBlock.IsZero() && r.Status != ui.BlockRejected {
			s.Pool.LastBlock = r.Time
		}
	}

	s.BestShares = a.roundBest(v, s.Pool.LastBlock, s.Pool.BestShare, nd)
	if s.Pool.BestShare == 0 && len(s.BestShares) > 0 {
		s.Pool.BestShare = s.BestShares[0].Diff
	}

	// The newest hour of one-minute points, wherever the log ends.
	hr := v.Hashrate
	if len(hr) > 60 {
		hr = hr[len(hr)-60:]
	}
	for _, pt := range hr {
		s.Hashrates = append(s.Hashrates, pt.V)
	}

	s.Network = a.net
	if s.Network.Difficulty == 0 {
		s.Network.Difficulty = nd
	}
	if a.logNet.zmq.After(s.Network.ZMQLast) {
		s.Network.ZMQLast = a.logNet.zmq
	}
	if s.Network.TemplateTime.IsZero() {
		s.Network.TemplateTime, s.Network.TemplateTxs = a.logNet.tmpl, a.logNet.tmplTxs
	}
	if s.Network.TipHash == "" {
		s.Network.TipHash, s.Network.TipTime = a.logNet.tipHash, a.logNet.tipHashTime
	}
	a.fillNetBlock(&s.Network, a.logNet.zmq, a.logNet.tipHashTime)
	switch {
	case st.shareHeight > 0:
		s.Pool.Height = int(st.shareHeight)
	case s.Network.Tip > 0:
		s.Pool.Height = s.Network.Tip + 1
	}

	from := max(len(st.recent)-ui.MaxShareDrops, 0)
	for _, sh := range st.recent[from:] {
		t, _ := shareTime(sh)
		s.Shares = append(s.Shares, ui.ShareDrop{Time: t, Worker: sh.WorkerName, Diff: sh.Diff, SDiff: sh.SDiff,
			Accepted: sh.Result, Near: sh.IsBlock(nd) || ui.IsNear(sh.SDiff, nd)})
	}

	if withFeed {
		s.Feed = append([]ui.FeedRow(nil), a.feed...)
		if st.noSharelogs {
			s.Feed = append(s.Feed, ui.FeedRow{Time: now, Style: "HINT", Colour: 5,
				Text: "no sharelog directories: enable logshares in the pool config for live share data and drill-down"})
		}
	}
	return s
}

func rigOf(name string) string {
	_, rig := poolfmt.SplitWorker(name)
	return rig
}

// roundBest is the per-worker leaderboard since the pool's last block. It
// comes from the sharelogs when they exist; otherwise from the log's
// per-minute user lines. Nothing at or before the last block is kept, and a
// candidate above the pool's own round best (or the network difficulty) is
// dropped and reported once rather than clamped.
func (a *App) roundBest(v index.View, last time.Time, poolBest, netDiff float64) []ui.BestShare {
	src := v.Best
	if v.RoundSeen {
		src = v.RoundBest
	}
	var out []ui.BestShare
	for _, b := range src {
		if b.Time.IsZero() || !b.Time.After(last) {
			continue
		}
		if (poolBest > 0 && b.Diff > poolBest*(1+1e-9)+1) || (netDiff > 0 && b.Diff >= netDiff) {
			a.warnBest(b)
			continue
		}
		out = append(out, ui.BestShare{Worker: b.Address, Diff: b.Diff, Time: b.Time})
		if len(out) == 10 {
			break
		}
	}
	return out
}

func (a *App) warnBest(b index.BestShare) {
	if a.bestWarned == nil {
		a.bestWarned = map[string]bool{}
	}
	k := b.Address + "|" + b.Time.Format(time.RFC3339Nano)
	if a.bestWarned[k] {
		return
	}
	a.bestWarned[k] = true
	if a.cfg.Log != nil {
		a.cfg.Log.Printf("best share %.0f from %s at %s exceeds the round best or network difficulty; dropped", b.Diff, b.Address, b.Time.Format(time.RFC3339))
	}
}
