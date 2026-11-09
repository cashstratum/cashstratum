package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const day = 24 * time.Hour

// compactMeterBelow is the screen height under which the meters drop their
// legend lines so the lower boxes keep room.
const compactMeterBelow = 30

func renderDashboard(ctx *Context, w, h int) []string {
	return renderDashboardSel(ctx, w, h, -1)
}

// renderDashboardSel draws the dashboard with row sel of the round table
// highlighted; a negative sel highlights none.
func renderDashboardSel(ctx *Context, w, h, sel int) []string {
	s := ctx.Snap
	p := s.Pool
	down := ctx.stale()
	if down {
		p = lastSeen(p)
	}

	cw := (w-2)/3 - 1 // the Pool box gets the spare columns: its labels are wider
	if w >= 110 {
		cw -= 3 // room for the two-digit worker counts next to the 16-wide labels
	}
	lw := w - 2 - 2*cw
	cardH := 11

	hr := []string{
		kv(ctx, "1m", 5, fit(fmtHash(p.Hashrate1m), 11)) + kv(ctx, "1h", 4, fmtHash(p.Hashrate1h)),
		kv(ctx, "5m", 5, fit(fmtHash(p.Hashrate5m), 11)) + kv(ctx, "6h", 4, fmtHash(p.Hashrate6h)),
		kv(ctx, "15m", 5, fit(fmtHash(p.Hashrate15m), 11)) + kv(ctx, "1d", 4, fmtHash(p.Hashrate1d)),
		kv(ctx, "", 5, fit("", 11)) + kv(ctx, "7d", 4, fmtHash(p.Hashrate7d)),
		"",
		sparklineOrIdle(ctx, s.Hashrates, cw-4),
		"",
		kv(ctx, "shares/s", 10, fmtSPS(p.SPS1m)+" 1m  "+fmtSPS(p.SPS5m)+" 5m"),
	}

	rate := poolRate(p)
	effort := p.RoundPercent
	if effort <= 0 {
		effort = PoolLuck(p.Accepted, p.NetworkDiff)
	}
	eta, etaOK := ETA(rate, p.NetworkDiff)
	etaS := dash
	if etaOK {
		etaS = fmtDur(eta)
	}
	best := fmtDiff(p.BestShare)
	if p.NetworkDiff > 0 && p.BestShare > 0 {
		best += " (" + fmtPct(p.BestShare/p.NetworkDiff*100) + " of net)"
	}
	since := "none yet, up " + fmtCoarse(p.Runtime)
	if !p.LastBlock.IsZero() {
		since = fmtCoarse(ctx.Now.Sub(p.LastBlock)) + " ago"
	}
	n7, n30 := blocksWithin(s.Blocks, ctx.Now, 7*day), blocksWithin(s.Blocks, ctx.Now, 30*day)
	round := []string{
		kv(ctx, "round effort", 15, ctx.Theme.Accent.Render(fmtPct(effort))),
		kv(ctx, "best share", 15, best),
		kv(ctx, "net diff", 15, diffChangeStyled(ctx, netDiff(s), s.Network.PrevDifficulty)),
		kv(ctx, "P(block 24h)", 15, probStr(down, rate, p.NetworkDiff, day)),
		kv(ctx, "P(block 7d)", 15, probStr(down, rate, p.NetworkDiff, 7*day)),
		kv(ctx, "expected every", 15, etaS),
		netLine(ctx, kv(ctx, "last net block", 15, netBlockAge(ctx, s.Network.LastNetBlock))),
		kv(ctx, "last block", 15, since),
		kv(ctx, "blocks 7d/30d", 15, itoa(n7)+" / "+itoa(n30)+"  (all "+itoa(len(s.Blocks))+")"),
	}

	if s.Prop.On {
		round = propRoundLines(ctx, round)
	}

	live, stale, idle, off := ctx.countStates(s.Workers)
	lastPool := coarseAgoOrDash(ctx.Now, p.LastBlock)
	topRound := dash
	if len(s.Round.Miners) > 0 {
		m := s.Round.Miners[0]
		topRound = shortAddr(m.Address, ctx.sym.Ellipsis) + " " + fmtPct(m.Pct)
	}
	rej := dash
	if tot := p.Accepted + p.Rejected; tot > 0 {
		rej = fmtPct(p.Rejected / tot * 100)
	}
	pool := []string{
		kv(ctx, "workers", 16, joinFit(ctx, lw-2-16,
			ctx.Theme.Good.Render(itoa(live)+" live"), itoa(stale)+" stale", itoa(idle)+" idle", itoa(off)+" off")),
		kv(ctx, "reported", 16, joinFit(ctx, lw-2-16,
			ctx.count(p.Workers)+" conn", ctx.count(p.Idle)+" idle", ctx.count(p.Disconnected)+" resum")),
		kv(ctx, "users", 16, ctx.count(p.Users)),
		kv(ctx, "uptime", 16, uptimeStr(ctx, p.Runtime)),
		kv(ctx, "height", 16, heightStr(p.Height)),
		kv(ctx, "rejected", 16, rej),
		kv(ctx, "last pool block", 16, lastPool),
		kv(ctx, "top round", 16, topRound),
	}

	out := hjoin(
		box(ctx, "Hashrate", hr, cw, cardH),
		box(ctx, "Round & odds", round, cw, cardH),
		box(ctx, "Pool", pool, lw, cardH),
	)
	bm, em := blockMeter(ctx, w-4), effortMeter(ctx, w-4)
	if h < compactMeterBelow {
		bm, em = bm[:min(1, len(bm))], em[:min(1, len(em))]
	}
	meters := append(bm, em...)
	out = append(out, box(ctx, "Block Meter", meters, w, len(meters)+2)...)
	out = append(out, healthStrip(ctx, w))

	rest := h - len(out)
	if rest >= 4 {
		half := (w - 1) / 2
		out = append(out, hjoin(
			box(ctx, roundTitle(ctx), roundTableSel(ctx, half-2, rest-2, sel), half, rest),
			box(ctx, "Recent blocks", recentBlocks(ctx, w-1-half-2, rest-2, etaS), w-1-half, rest),
		)...)
	}
	return out
}

// netDiff is the network difficulty, from the node when known.
func netDiff(s *Snapshot) float64 {
	if s.Network.Difficulty > 0 {
		return s.Network.Difficulty
	}
	return s.Pool.NetworkDiff
}

// netLine highlights the line briefly after a new network block.
func netLine(ctx *Context, line string) string {
	if ctx.NetFlash {
		return ctx.Theme.Selected.Render(line)
	}
	return line
}

// poolRate prefers the short-window hashrate and falls back to longer ones.
func poolRate(p PoolCard) float64 {
	for _, v := range []float64{p.Hashrate1h, p.Hashrate15m, p.Hashrate5m, p.Hashrate1m, p.Hashrate1d} {
		if v > 0 {
			return v
		}
	}
	return 0
}

// lastSeen blanks every figure that is only meaningful while the pool runs, so
// the cards render them as unknown. The snapshot itself keeps the values.
func lastSeen(p PoolCard) PoolCard {
	p.Hashrate1m, p.Hashrate5m, p.Hashrate15m, p.Hashrate1h = 0, 0, 0, 0
	p.Hashrate6h, p.Hashrate1d, p.Hashrate7d = 0, 0, 0
	p.SPS1m, p.SPS5m = 0, 0
	p.Runtime, p.AllAliveSince = 0, time.Time{}
	p.LongestSession, p.LongestSessionName = 0, ""
	return p
}

// probStr is the chance of a block within d, unknown while the pool is down.
func probStr(down bool, rate, netDiff float64, d time.Duration) string {
	if down {
		return dash
	}
	return fmtPct(ProbBlockWithin(rate, netDiff, d) * 100)
}

func uptimeStr(ctx *Context, d time.Duration) string {
	if ctx.stale() || d <= 0 {
		return dash
	}
	return fmtCoarse(d)
}

func sparklineOrIdle(ctx *Context, v []float64, w int) string {
	if ctx.stale() {
		return ctx.Theme.Dim.Render("no live data")
	}
	return sparkline(ctx, v, w)
}

func fmtSPS(v float64) string {
	if v <= 0 {
		return dash
	}
	return fmtSI(v)
}

func heightStr(h int) string {
	if h <= 0 {
		return dash
	}
	return itoa(h)
}

func blocksWithin(bs []BlockRow, now time.Time, d time.Duration) int {
	n := 0
	for _, b := range bs {
		if !b.Time.IsZero() && now.Sub(b.Time) <= d && b.Status != BlockRejected {
			n++
		}
	}
	return n
}

func healthStrip(ctx *Context, w int) string {
	n := ctx.Snap.Network
	t := ctx.Theme
	sep := t.Dim.Render(" " + ctx.sym.Sep + " ")
	node := t.Bad.Render(ctx.sym.Off + " node down")
	if n.RPCOK {
		node = t.Good.Render(ctx.sym.Live + " node ok")
	}
	parts := []string{
		node,
		t.Dim.Render("tip ") + heightStr(n.Tip),
		t.Dim.Render("rpc ") + fmtLatency(n.RPCLatency),
		t.Dim.Render("zmq ") + zmqLabel(ctx.Now, n),
		t.Dim.Render("template ") + ageCoarse(ctx.Now, n.TemplateTime) + " " + itoa(n.TemplateTxs) + " tx",
		t.Dim.Render("mempool ") + fmtInt(int64(n.MempoolTx)) + " tx",
	}
	return fit(" "+strings.Join(parts, sep), w)
}

func fmtLatency(d time.Duration) string {
	if d <= 0 {
		return dash
	}
	if d < time.Millisecond {
		return "<1ms"
	}
	return d.Round(time.Millisecond).String()
}

// zmqLabel names the state of the node's hashblock feed. A last-seen time
// alone is not enough: the log fallback can set it with no endpoint, and an
// endpoint that never connected has no time at all.
func zmqLabel(now time.Time, n NetworkInfo) string {
	if n.ZMQLast.IsZero() {
		switch {
		case !n.ZMQConfigured:
			return "off"
		case !n.ZMQConnected:
			return "down"
		default:
			return "waiting"
		}
	}
	if n.ZMQConfigured && !n.ZMQConnected {
		return ageCoarse(now, n.ZMQLast) + " down"
	}
	return ageCoarse(now, n.ZMQLast)
}

// ageCoarse renders an age at ten-second steps so idle screens stay still.
func ageCoarse(now, t time.Time) string {
	if t.IsZero() {
		return dash
	}
	d := now.Sub(t)
	if d < time.Minute {
		return itoa(int(d/(10*time.Second))*10) + "s"
	}
	return fmtCoarse(d)
}

// coarseAgoOrDash is the age of t as "<span> ago", or a dash when unknown.
func coarseAgoOrDash(now, t time.Time) string {
	if t.IsZero() {
		return dash
	}
	return fmtCoarse(now.Sub(t)) + " ago"
}

// roundTitle names the round table: the last block's height and how long the
// round has been running.
func roundTitle(ctx *Context) string {
	s := ctx.Snap
	h := dash
	if len(s.Blocks) > 0 {
		h = itoa(s.Blocks[0].Height)
	}
	since := s.Round.Since
	if since.IsZero() {
		since = s.Pool.LastBlock
	}
	return "Round since last block (" + h + ", " + coarseAgoOrDash(ctx.Now, since) + ")"
}

var roundCols = []column{
	{title: "#", width: 3, prio: 0},
	{title: "address", width: 12, prio: 0, flex: 20},
	{title: "% round", width: 8, right: true, prio: 0},
	{title: "effort", width: 8, right: true, prio: 1},
	{title: "best", width: 8, right: true, prio: 2},
	{title: "of net", width: 9, right: true, prio: 3},
}

// roundTable lists each address's slice of the round in progress, biggest
// first.
func roundTable(ctx *Context, w, h int) []string { return roundTableSel(ctx, w, h, -1) }

// roundTableSel is roundTable with row sel highlighted; negative highlights none.
func roundTableSel(ctx *Context, w, h, sel int) []string {
	r := ctx.Snap.Round
	if len(r.Miners) == 0 {
		return []string{ctx.Theme.Dim.Render("no shares since the last block yet")}
	}
	rc := roundCols
	if ctx.Snap.Prop.On {
		rc = propRoundCols
	}
	keep := fitColumns(rc, w)
	cols := growFlex(rc, keep, w)
	out := []string{tableHead(ctx, cols, keep)}
	for i, m := range r.Miners {
		if i >= h-1 {
			break
		}
		var cells []string
		if ctx.Snap.Prop.On {
			cells = propRoundRow(ctx, i, m)
		} else {
			effort := dash
			if m.OwnLuckOK {
				effort = fmtPct(m.OwnLuck)
			}
			cells = []string{itoa(i + 1), shortAddr(m.Address, ctx.sym.Ellipsis),
				fmtPct(m.Pct), effort, fmtDiff(m.Best), fmtOfNet(m.BestOfNet)}
		}
		row := tableRow(cols, keep, cells)
		st := ctx.Theme.Text
		if i == 0 {
			st = ctx.Theme.Gold
		}
		if i == sel {
			st = ctx.Theme.Selected
		}
		out = append(out, st.Render(row))
	}
	return out
}

var recentCols = []column{
	{title: "height", width: 8, prio: 0},
	{title: "when", width: 7, prio: 3},
	{title: "found by", width: 12, prio: 4, flex: 24},
	{title: "finder", width: 7, right: true, prio: 0},
	{title: "pool", width: 7, right: true, prio: 1},
	{title: "status", width: 9, prio: 2},
}

func recentBlocks(ctx *Context, w, h int, eta string) []string {
	s := ctx.Snap
	t := ctx.Theme
	if len(s.Blocks) == 0 {
		rate := poolRate(s.Pool)
		return []string{
			t.Text.Render("No blocks found yet."),
			"",
			kv(ctx, "expected every", 16, eta),
			kv(ctx, "P(block 24h)", 16, fmtPct(ProbBlockWithin(rate, s.Pool.NetworkDiff, day)*100)),
			kv(ctx, "P(block 30d)", 16, fmtPct(ProbBlockWithin(rate, s.Pool.NetworkDiff, 30*day)*100)),
			kv(ctx, "hunting for", 16, fmtCoarse(s.Pool.Runtime)),
		}
	}
	keep := fitColumns(recentCols, w)
	cols := growFlex(recentCols, keep, w)
	out := []string{tableHead(ctx, cols, keep)}
	for i, b := range s.Blocks {
		if i >= h-1 {
			break
		}
		row := tableRow(cols, keep, []string{itoa(b.Height), coarseAgo(ctx.Now, b.Time),
			shortWorker(b.Finder, ctx.sym.Ellipsis), finderEffort(ctx, b, !badBlock(b)), poolEffort(b), statusStyled(ctx, b.Status, 9)})
		out = append(out, row)
	}
	return out
}

func statusStyled(ctx *Context, s BlockStatus, w int) string {
	t := ctx.Theme
	v := fit(string(s), w)
	switch s {
	case BlockConfirmed:
		return t.Good.Render(v)
	case BlockOrphaned, BlockRejected:
		return t.Bad.Render(v)
	case BlockPending:
		return t.Warn.Render(v)
	}
	return t.Dim.Render(fit(dash, w))
}

// joinFit joins segs with the separator and drops trailing segments until the
// result fits width cells, so a value is never cut mid-word.
func joinFit(ctx *Context, width int, segs ...string) string {
	for n := len(segs); n > 1; n-- {
		out := strings.Join(segs[:n], ctx.sym.Sep)
		if ansi.StringWidth(out) <= width {
			return out
		}
	}
	return segs[0]
}
