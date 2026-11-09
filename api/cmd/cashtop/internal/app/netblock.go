package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"cashstratumapi/cmd/cashtop/internal/pooltags"
	"cashstratumapi/cmd/cashtop/internal/rpc"
	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/poolindex"
)

// netTrack follows the chain tip: when it last moved, the difficulty of the
// tip and of its parent, and the consecutive difficulties the log reported.
type netTrack struct {
	mu sync.Mutex

	hash     string // tip whose headers were last read, or announced from the log
	announce string // newest tip already announced to the UI
	tipTime  time.Time
	prevTime time.Time
	diff     float64
	prevDiff float64

	lastBlock time.Time // newest log or notification sign of a network block

	logDiff     float64
	logPrevDiff float64

	recent   map[string]recentBlock // network blocks by hash
	recentTo string                 // tip the recent window was last built from
	window   []string               // hashes of the window, newest first
	sig      string                 // the pool's own coinbase signature, lowercase
	sigRead  bool
}

// recentBlock is one cached network block.
type recentBlock struct {
	height int
	time   time.Time
	diff   float64
	prev   string
	tag    string
	pool   string
}

const (
	recentKeep   = 20 // blocks shown on the network tab
	recentBudget = 6  // uncached blocks fetched per refresh
)

func (a *App) track() *netTrack { return &a.nb }

// noteLogDiff records one "Network diff set" line.
func (a *App) noteLogDiff(diff float64, at time.Time) {
	if diff <= 0 {
		return
	}
	t := a.track()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logPrevDiff, t.logDiff = t.logDiff, diff
	if at.After(t.lastBlock) {
		t.lastBlock = at
	}
}

// noteNetworkBlock records a tip change seen in the log and returns the
// message announcing it, unless that tip was already announced.
func (a *App) noteNetworkBlock(hash string, at time.Time) (ui.NetworkBlockMsg, bool) {
	a.mu.Lock()
	tip := a.net.Tip
	a.mu.Unlock()
	t := a.track()
	t.mu.Lock()
	defer t.mu.Unlock()
	prev := t.lastBlock
	if t.tipTime.After(prev) {
		prev = t.tipTime
	}
	if at.After(t.lastBlock) {
		t.lastBlock = at
	}
	if hash != "" && t.announce == hash {
		return ui.NetworkBlockMsg{}, false
	}
	t.announce = hash
	m := ui.NetworkBlockMsg{Time: at, Diff: t.logDiff, PrevDiff: t.logPrevDiff}
	if tip > 0 {
		m.Height = tip + 1
	}
	if !prev.IsZero() && at.After(prev) {
		m.Gap = at.Sub(prev)
	}
	return m, true
}

// refreshTip reads the headers of the node's tip and its parent when the tip
// changed. It returns the announcement when the log has not already made it.
func (a *App) refreshTip(ctx context.Context) (ui.NetworkBlockMsg, bool) {
	if a.node == nil {
		return ui.NetworkBlockMsg{}, false
	}
	return a.refreshTipWith(ctx, a.node.BlockHeaderDiff)
}

func (a *App) refreshTipWith(ctx context.Context, fetch func(context.Context, string) (rpc.HeaderDiff, error)) (ui.NetworkBlockMsg, bool) {
	a.mu.Lock()
	hash, height := a.net.TipHash, a.net.Tip
	ok := a.net.RPCOK
	a.mu.Unlock()
	t := a.track()
	t.mu.Lock()
	known := t.hash == hash
	first := t.hash == "" && t.announce == ""
	t.mu.Unlock()
	if !ok || hash == "" || known {
		return ui.NetworkBlockMsg{}, false
	}
	c, cancel := context.WithTimeout(ctx, 2*rpcTimeout)
	defer cancel()
	h, err := fetch(c, hash)
	if err != nil {
		return ui.NetworkBlockMsg{}, false
	}
	var p struct {
		time int64
		diff float64
	}
	if h.PreviousHash != "" {
		if ph, err := fetch(c, h.PreviousHash); err == nil {
			p.time, p.diff = ph.Time, ph.Difficulty
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hash = hash
	t.diff, t.prevDiff = h.Difficulty, p.diff
	t.tipTime, t.prevTime = unixTime(h.Time), unixTime(p.time)
	if first || t.announce == hash {
		t.announce = hash
		return ui.NetworkBlockMsg{}, false
	}
	t.announce = hash
	m := ui.NetworkBlockMsg{Height: height, Time: t.tipTime, Diff: t.diff, PrevDiff: t.prevDiff}
	if !t.tipTime.IsZero() && !t.prevTime.IsZero() && t.tipTime.After(t.prevTime) {
		m.Gap = t.tipTime.Sub(t.prevTime)
	}
	return m, true
}

// refreshRecent keeps the window of the newest network blocks. It walks back
// from the tip through previousblockhash, reusing cached blocks and fetching
// at most recentBudget new ones per call, newest first, so the window is
// always contiguous from the tip and deepens over successive refreshes.
func (a *App) refreshRecent(ctx context.Context) {
	if a.node == nil {
		return
	}
	a.refreshRecentWith(ctx, a.node.BlockCoinbase)
}

func (a *App) refreshRecentWith(ctx context.Context, fetch func(context.Context, string) (rpc.Coinbase, error)) {
	a.mu.Lock()
	tip, ok := a.net.TipHash, a.net.RPCOK
	a.mu.Unlock()
	if !ok || tip == "" {
		return
	}
	t := a.track()
	t.mu.Lock()
	if t.recent == nil {
		t.recent = map[string]recentBlock{}
	}
	if !t.sigRead {
		t.sigRead = true
		t.sig = strings.ToLower(readBtcSig(a.cfg.Conf))
	}
	done := t.recentTo == tip && len(t.window) >= recentKeep
	t.mu.Unlock()
	if done {
		return
	}
	budget := recentBudget
	window := make([]string, 0, recentKeep)
	for cur := tip; cur != "" && len(window) < recentKeep; {
		t.mu.Lock()
		b, have := t.recent[cur]
		t.mu.Unlock()
		if !have {
			if budget == 0 || ctx.Err() != nil {
				break
			}
			budget--
			c, cancel := context.WithTimeout(ctx, rpcTimeout)
			cb, err := fetch(c, cur)
			cancel()
			if err != nil {
				break
			}
			tag := pooltags.Decode(cb.ScriptHex)
			pool, _ := pooltags.Match(tag)
			b = recentBlock{height: int(cb.Height), time: unixTime(cb.Time), diff: cb.Difficulty, prev: cb.PreviousHash, tag: tag, pool: pool}
			t.mu.Lock()
			t.recent[cur] = b
			t.mu.Unlock()
		}
		window = append(window, cur)
		cur = b.prev
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.window, t.recentTo = window, tip
	// Keep the cache bounded: drop blocks that left the window long ago.
	if len(t.recent) > 4*recentKeep {
		keep := make(map[string]recentBlock, len(window))
		for _, h := range window {
			keep[h] = t.recent[h]
		}
		t.recent = keep
	}
}

// readBtcSig reads the coinbase signature the pool puts in its blocks from
// the pool configuration; empty when unset or unreadable.
func readBtcSig(conf string) string {
	if conf == "" {
		return ""
	}
	data, err := os.ReadFile(conf)
	if err != nil {
		return ""
	}
	var c struct {
		BtcSig string `json:"btcsig"`
	}
	if json.Unmarshal(data, &c) != nil {
		return ""
	}
	return strings.TrimSpace(c.BtcSig)
}

// fillRecent copies the window into n. A block is ours when its hash is one
// the pool found itself or its coinbase carries the pool's own signature.
// The caller holds t.mu.
func (a *App) fillRecent(n *ui.NetworkInfo) {
	t := a.track()
	if len(t.window) == 0 || t.recentTo != n.TipHash {
		return
	}
	mine := map[string]bool{}
	for _, b := range a.ix.Snapshot().Blocks {
		if b.Hash != "" && b.Status != poolindex.StatusOrphaned {
			mine[b.Hash] = true
		}
	}
	rows := make([]ui.NetBlockRow, 0, len(t.window))
	for i, h := range t.window {
		b := t.recent[h]
		r := ui.NetBlockRow{Height: b.height, Hash: h, Time: b.time, Pool: b.pool, Tag: b.tag}
		if i+1 < len(t.window) {
			if p := t.recent[t.window[i+1]]; !p.time.IsZero() && b.time.After(p.time) {
				r.Interval = b.time.Sub(p.time)
			}
		}
		if mine[h] || (t.sig != "" && strings.Contains(strings.ToLower(b.tag), t.sig)) {
			r.Ours, r.Pool = true, "CashStratum"
		}
		rows = append(rows, r)
	}
	n.Recent = rows
	series := make([]float64, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		if d := t.recent[rows[i].Hash].diff; d > 0 {
			series = append(series, d)
		}
	}
	n.DiffSeries = series
}

func unixTime(s int64) time.Time {
	if s <= 0 {
		return time.Time{}
	}
	return time.Unix(s, 0).UTC()
}

// fillNetBlock adds the network-block fields to n: the newest sign of a
// block from the tip header, the notification and the log, and the previous
// difficulty from the node, else from the log's last two difficulties.
func (a *App) fillNetBlock(n *ui.NetworkInfo, logZMQ, logTip time.Time) {
	t := a.track()
	t.mu.Lock()
	defer t.mu.Unlock()
	last := n.TipTime
	for _, c := range []time.Time{n.ZMQLast, logZMQ, logTip, t.lastBlock, t.tipTime} {
		if c.After(last) {
			last = c
		}
	}
	n.LastNetBlock = last
	a.fillRecent(n)
	if t.hash != "" && t.hash == n.TipHash && t.prevDiff > 0 {
		n.PrevDifficulty, n.PrevTipTime = t.prevDiff, t.prevTime
		if n.Difficulty == 0 {
			n.Difficulty = t.diff
		}
		return
	}
	if t.logPrevDiff > 0 && n.Difficulty > 0 && t.logDiff == n.Difficulty {
		n.PrevDifficulty = t.logPrevDiff
	}
}
