package ui

import (
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

func renderWorkers(ctx *Context, w, h, sel int) []string {
	t := ctx.Theme
	ws := sortedWorkers(ctx.Snap.Workers, ctx.Now)
	live, stale, off := ctx.countStates(ws)
	sep := " " + ctx.sym.Sep + " "
	out := []string{
		" " + t.Title.Render("Workers") + "  " + t.Good.Render(itoa(live)+" live") + sep + t.Warn.Render(itoa(stale)+" stale") + sep + t.Dim.Render(itoa(off)+" off") +
			t.Dim.Render("   stale = no share for "+itoa(StaleK)+"x the expected interval"),
		"",
	}
	if len(ws) == 0 {
		return append(out, " "+t.Text.Render("No workers connected."), "",
			" "+t.Dim.Render("Workers appear here once a miner authorises and submits a share."))
	}
	keep := fitColumns(workerCols, w-1)
	out = append(out, " "+tableHead(ctx, workerCols, keep))
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
			cur, badge + " " + tag, r.Name, shortAddr(r.Address, ctx.sym.Ellipsis), agentShort(r.UserAgent),
			ctx.hash(r.Hashrate1m), ctx.hash(r.Hashrate1h), ctx.hash(r.Hashrate1d),
			fmtDiff(r.Diff), fmtDiff(r.BestShare), last, stateLabel(ctx, st),
		}
		row := tableRow(workerCols, keep, cells)
		switch {
		case i == sel:
			row = t.Selected.Render(row)
		case st == StateOff:
			row = t.Dim.Render(row)
		}
		out = append(out, " "+row)
	}
	return out
}

func stateLabel(ctx *Context, s WorkerState) string {
	switch s {
	case StateLive:
		return ctx.sym.Live + " live"
	case StateStale:
		return ctx.sym.Stale + " stale"
	}
	return ctx.sym.Off + " off"
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
