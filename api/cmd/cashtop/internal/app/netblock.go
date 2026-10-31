package app

import (
	"context"
	"sync"
	"time"

	"cashstratumapi/cmd/cashtop/internal/rpc"
	"cashstratumapi/cmd/cashtop/internal/ui"
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
}

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
