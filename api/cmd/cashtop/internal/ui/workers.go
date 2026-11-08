package ui

import (
	"github.com/charmbracelet/x/ansi"
	"sort"
	"strings"
	"time"

	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui/glyphs"
)

// sortedWorkers orders live workers first, then by hashrate, then by name.
func sortedWorkers(ws []WorkerRow, now time.Time) []WorkerRow {
	out := append([]WorkerRow(nil), ws...)
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := Classify(out[i], now), Classify(out[j], now)
		if si != sj {
			return si < sj
		}
		if out[i].Hashrate1m != out[j].Hashrate1m {
			return out[i].Hashrate1m > out[j].Hashrate1m
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// visibleWorkers is sortedWorkers without the off workers unless showOff.
// Row selection in the Workers tab indexes this list.
func visibleWorkers(ws []WorkerRow, now time.Time, showOff bool) []WorkerRow {
	all := sortedWorkers(ws, now)
	if showOff {
		return all
	}
	out := all[:0]
	for _, w := range all {
		if Classify(w, now) != StateOff {
			out = append(out, w)
		}
	}
	return out
}

func rigBadge(ctx *Context, w WorkerRow) (string, string) {
	c := glyphs.Infer(w.UserAgent, w.Name)
	return glyphs.Badge(c, ctx.Caps.Glyphs == termcap.ASCII), c.Tag()
}

var workerCols = []column{
	{title: "", width: 1, prio: 0},
	{title: "rig", width: 7, prio: 0},
	{title: "worker", width: 20, prio: 0},
	{title: "address", width: 14, prio: 3},
	{title: "agent", width: 16, prio: 4},
	{title: "1m", width: 10, right: true, prio: 0},
	{title: "1h", width: 10, right: true, prio: 2},
	{title: "1d", width: 10, right: true, prio: 5},
	{title: "diff", width: 7, right: true, prio: 1},
	{title: "best", width: 7, right: true, prio: 6},
	{title: "last share", width: 10, right: true, prio: 0},
	{title: "state", width: 7, prio: 0},
}

// renderWorkers draws the Workers tab with off workers hidden.
func renderWorkers(ctx *Context, w, h, sel int) []string {
	return renderWorkersShow(ctx, w, h, sel, false)
}

// renderWorkersShow draws the Workers tab. Off workers are listed only when
// showOff is set or the pool itself is down (then every worker is off and
// hiding them would leave nothing to read). sel indexes visibleWorkers.
func renderWorkersShow(ctx *Context, w, h, sel int, showOff bool) []string {
	t := ctx.Theme
	showOff = showOff || ctx.stale()
	all := sortedWorkers(ctx.Snap.Workers, ctx.Now)
	live, stale, idle, off := ctx.countStates(all)
	ws := all
	if !showOff {
		ws = visibleWorkers(ctx.Snap.Workers, ctx.Now, false)
	}
	sep := " " + ctx.sym.Sep + " "
	offTxt := itoa(off) + " off"
	switch {
	case off > 0 && !showOff:
		offTxt += " (hidden)"
	}
	out := []string{
		" " + t.Title.Render("Workers") + "  " + t.Good.Render(itoa(live)+" live") + sep + t.Warn.Render(itoa(stale)+" stale") + sep + t.Dim.Render(itoa(idle)+" idle") + sep + t.Dim.Render(offTxt) +
			t.Dim.Render("   stale = no share for "+itoa(StaleK)+"x the expected interval"),
		"",
	}
	if len(ws) == 0 {
		return append(out, " "+t.Text.Render("No workers connected."), "",
			" "+t.Dim.Render("Workers appear here once a miner authorises and submits a share."))
	}
	cols := withStateWidth(workerCols, stateWidth(ctx, ws))
	keep := fitColumns(cols, w-1)
	out = append(out, " "+tableHead(ctx, cols, keep))
	lo, hi := window(len(ws), h-len(out), sel)
	for i := lo; i < hi; i++ {
		r := ws[i]
		badge, tag := rigBadge(ctx, r)
		st := ctx.classify(r)
		cur := " "
		if i == sel {
			cur = ctx.sym.Cursor
		}
		last := dash
		if !r.LastShare.IsZero() {
			last = fmtDur(ctx.Now.Sub(r.LastShare))
		}
		cells := []string{
			cur, badge + " " + tag, shortWorker(r.Name, ctx.sym.Ellipsis), shortAddr(r.Address, ctx.sym.Ellipsis), agentCell(ctx, r),
			ctx.hash(r.Hashrate1m), ctx.longHash(r, r.Hashrate1h), ctx.longHash(r, r.Hashrate1d),
			fmtDiff(r.Diff), fmtDiff(r.BestShare), last, workerState(ctx, r, st),
		}
		row := tableRow(cols, keep, cells)
		switch {
		case i == sel:
			row = t.Selected.Render(row)
		case st == StateOff || st == StateIdle:
			row = t.Dim.Render(row)
		}
		out = append(out, " "+row)
	}
	return out
}

// stateWidth is the width of the state column: wide enough for the
// "off <ago> · <reason>" cells of the rows shown, else the plain label.
func stateWidth(ctx *Context, ws []WorkerRow) int {
	n := 7
	for _, r := range ws {
		n = max(n, ansi.StringWidth(workerState(ctx, r, ctx.classify(r))))
	}
	return min(n, 28)
}

// withStateWidth copies cols with the state column (the last) resized.
func withStateWidth(cols []column, w int) []column {
	out := append([]column(nil), cols...)
	out[len(out)-1].width = w
	return out
}

func stateLabel(ctx *Context, s WorkerState) string {
	switch s {
	case StateLive:
		return ctx.sym.Live + " live"
	case StateStale:
		return ctx.sym.Stale + " stale"
	case StateIdle:
		return ctx.sym.Idle + " idle"
	}
	return ctx.sym.Off + " off"
}

// workerState is the state cell of a worker row. A worker that dropped reads
// "off <ago> · <reason>" so the operator sees why it left.
func workerState(ctx *Context, r WorkerRow, st WorkerState) string {
	label := stateLabel(ctx, st)
	if st != StateOff || !dropped(r) || ctx.stale() {
		return label
	}
	label += " " + ageSince(ctx.Now, r.Disconnected)
	if r.DropReason != "" {
		label += " " + ctx.sym.Sep + " " + r.DropReason
	}
	return label
}

// agentCell is the agent of a row, dimmed when it is only the last one known.
func agentCell(ctx *Context, r WorkerRow) string {
	a := agentShort(r.UserAgent)
	if r.AgentStale && r.UserAgent != "" {
		return ctx.Theme.Dim.Render(a)
	}
	return a
}

func agentShort(a string) string {
	if a == "" {
		return dash
	}
	if i := strings.IndexAny(a, " ("); i > 0 {
		a = a[:i]
	}
	return a
}
