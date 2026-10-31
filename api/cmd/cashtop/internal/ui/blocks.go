package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

var blockCols = []column{
	{title: "", width: 1, prio: 0},
	{title: "height", width: 8, prio: 0},
	{title: "found (UTC)", width: 16, prio: 0},
	{title: "ago", width: 8, prio: 3},
	{title: "finder", width: 22, prio: 0},
	{title: "proto", width: 5, prio: 5},
	{title: "effort", width: 8, right: true, prio: 0},
	{title: "status", width: 9, prio: 0},
	{title: "conf", width: 5, right: true, prio: 4},
	{title: "reward", width: 12, right: true, prio: 2},
	{title: "hash", width: 17, prio: 6},
}

func renderBlocks(ctx *Context, w, h, sel int) []string {
	t := ctx.Theme
	bs := ctx.Snap.Blocks
	out := []string{" " + t.Title.Render("Blocks") + "  " + t.Text.Render(itoa(len(bs))+" found") +
		t.Dim.Render("   enter: sharelog for the height "+ctx.sym.Sep+" /: search by height"), ""}
	if len(bs) == 0 {
		p := ctx.Snap.Pool
		rate := poolRate(p)
		eta, ok := ETA(rate, p.NetworkDiff)
		etaS := dash
		if ok {
			etaS = fmtDur(eta)
		}
		return append(out,
			" "+t.Text.Render("No blocks found yet."), "",
			" "+kv(ctx, "round effort", 16, fmtPct(PoolLuck(p.Accepted, p.NetworkDiff))),
			" "+kv(ctx, "expected every", 16, etaS),
			" "+kv(ctx, "P(block 24h)", 16, fmtPct(ProbBlockWithin(rate, p.NetworkDiff, day)*100)),
			" "+kv(ctx, "best share", 16, fmtDiff(p.BestShare)+" of "+fmtDiff(p.NetworkDiff)),
		)
	}
	keep := fitColumns(blockCols, w-1)
	out = append(out, " "+tableHead(ctx, blockCols, keep))
	lo, hi := window(len(bs), h-len(out), sel)
	for i := lo; i < hi; i++ {
		b := bs[i]
		cur := " "
		if i == sel {
			cur = ctx.sym.Cursor
		}
		conf := dash
		if b.Confirmations > 0 {
			conf = itoa(b.Confirmations)
		}
		cells := []string{
			cur, itoa(b.Height), fmtStamp(b.Time), coarseAgo(ctx.Now, b.Time),
			shortWorker(b.Finder, ctx.sym.Ellipsis), b.Proto, fmtPct(b.Luck), string(b.Status), conf,
			fmtBCH(b.Reward), shortHash(b.Hash, 8, ctx.sym.Ellipsis),
		}
		row := tableRow(blockCols, keep, cells)
		if i == sel {
			row = t.Selected.Render(row)
		} else if b.Status == BlockOrphaned || b.Status == BlockRejected {
			row = t.Bad.Render(row)
		}
		out = append(out, " "+row)
	}
	return out
}

// drillPage lists the sharelog jobs of one height with the winning share
// highlighted.
type drillPage struct {
	height int
	data   *DrillDown
	err    error
	scroll int
}

func (p *drillPage) Title() string { return "height " + itoa(p.height) }

func (p *drillPage) Update(msg tea.Msg, _ *Context) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case DrillDownMsg:
		if msg.Height == p.height {
			p.data, p.err = &msg.Data, msg.Err
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			p.scroll++
		case "k", "up":
			p.scroll = max(p.scroll-1, 0)
		case "pgdown":
			p.scroll += 10
		case "pgup":
			p.scroll = max(p.scroll-10, 0)
		case "g", "home":
			p.scroll = 0
		}
	}
	return p, nil
}

var shareCols = []column{
	{title: "", width: 4, prio: 0},
	{title: "time (UTC)", width: 12, prio: 0},
	{title: "worker", width: 24, prio: 0},
	{title: "agent", width: 16, prio: 3},
	{title: "diff", width: 8, right: true, prio: 0},
	{title: "sdiff", width: 8, right: true, prio: 0},
	{title: "of net", width: 8, right: true, prio: 2},
	{title: "result", width: 16, prio: 0},
}

func (p *drillPage) Render(ctx *Context, w, h int) []string {
	t := ctx.Theme
	head := " " + t.Title.Render("Height "+itoa(p.height)) + t.Dim.Render("  sharelog dir ") + t.Text.Render(sharelogDir(p.height))
	switch {
	case p.err != nil:
		return []string{head, "", " " + t.Bad.Render("cannot read sharelog: "+p.err.Error())}
	case p.data == nil:
		return []string{head, "", " " + t.Dim.Render("reading sharelog"+ctx.sym.Ellipsis)}
	}
	d := p.data
	shares, wins := 0, 0
	for _, j := range d.Jobs {
		shares += len(j.Shares)
		for _, s := range j.Shares {
			if s.Winning {
				wins++
			}
		}
	}
	sep := t.Dim.Render(" " + ctx.sym.Sep + " ")
	head += sep + t.Dim.Render("net diff ") + fmtDiff(d.NetworkDiff) + sep + itoa(len(d.Jobs)) + " jobs" + sep + itoa(shares) + " shares"
	if wins > 0 {
		head += sep + t.Gold.Render(itoa(wins)+" winning")
	}
	if len(d.Jobs) == 0 {
		return []string{head, "", " " + t.Dim.Render("No sharelog for this height (logshares off, or rotated away).")}
	}
	keep := fitColumns(shareCols, w-1)
	var body []string
	for _, j := range d.Jobs {
		body = append(body, " "+t.Accent.Render("job "+j.ID)+t.Dim.Render("  "+itoa(len(j.Shares))+" shares"))
		for _, s := range j.Shares {
			mark := ""
			if s.Winning {
				mark = ctx.sym.Up + "WIN"
			}
			res := "accepted"
			if !s.Result {
				res = "rejected"
				if s.Error != "" {
					res = s.Error
				}
			}
			pct := dash
			if d.NetworkDiff > 0 {
				pct = fmtOfNet(s.SDiff / d.NetworkDiff * 100)
			}
			row := tableRow(shareCols, keep, []string{mark, s.Time.UTC().Format("15:04:05.000"),
				shortWorker(s.Worker, ctx.sym.Ellipsis), agentShort(s.Agent), fmtDiff(s.Diff), fmtDiff(s.SDiff), pct, res})
			switch {
			case s.Winning:
				row = t.Gold.Render(row)
			case !s.Result:
				row = t.Bad.Render(row)
			}
			body = append(body, " "+row)
		}
	}
	out := []string{head, "", " " + tableHead(ctx, shareCols, keep)}
	room := h - len(out)
	if p.scroll > len(body)-room {
		p.scroll = max(len(body)-room, 0)
	}
	end := min(len(body), p.scroll+room)
	return append(out, body[p.scroll:end]...)
}

// sharelogDir is the pool's per-height log directory name.
func sharelogDir(h int) string { return fmt.Sprintf("%08x", h) }
