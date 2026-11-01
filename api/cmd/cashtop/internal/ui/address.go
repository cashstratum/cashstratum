package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"cashstratumapi/internal/poolfmt"
)

func init() { addressPage = func(addr string) Page { return &addressView{addr: addr} } }

// AddressData is what the address view needs beyond the Snapshot. A
// Provider that can supply it implements AddressSource; without it the page
// shows the snapshot's workers and a hint that sharelogs are not available.
type AddressData struct {
	// Height is the network height whose sharelog directory Work sums.
	Height int64
	// Work is the accepted share difficulty of the address in that
	// directory; it starts over whenever a new height directory starts.
	Work float64
	// Submits are the address's recent sharelog lines, oldest first.
	Submits []poolfmt.Share
	// LogShares reports whether sharelog files exist for the current height.
	LogShares bool
	// LastShare maps a full worker name to its newest sharelog time, for
	// sub-minute liveness; it may be nil.
	LastShare map[string]time.Time
}

// AddressSource is optionally implemented by a Provider.
type AddressSource interface {
	Address(addr string) AddressData
}

// RoundWork is the share of the network difficulty the address has
// accepted at one height, as a percentage: the sum of diff over its
// accepted lines divided by networkDiff. Lines of other heights are ignored,
// so the value starts over when a new height directory starts.
func RoundWork(byHeight map[int64][]poolfmt.Share, addr string, height int64, networkDiff float64) float64 {
	if networkDiff <= 0 {
		return 0
	}
	var sum float64
	for _, s := range byHeight[height] {
		if s.Result && sameAddr(sharePayout(s), addr) {
			sum += s.Diff
		}
	}
	return sum / networkDiff * 100
}

// sharePayout is the address a share is credited to: the sharelog's
// username, or the address part of the worker name. The "address" field is
// the miner's IP and never identifies the payout.
func sharePayout(s poolfmt.Share) string {
	if s.UserName != "" {
		return s.UserName
	}
	a, _ := poolfmt.SplitWorker(s.WorkerName)
	return a
}

func sameAddr(a, b string) bool {
	return strings.TrimPrefix(a, "bitcoincash:") == strings.TrimPrefix(b, "bitcoincash:")
}

type addressView struct {
	addr string
	sel  int
}

func (a *addressView) Title() string { return "address" }

func (a *addressView) Update(msg tea.Msg, ctx *Context) (Page, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "down", "j":
			a.sel++
		case "up", "k":
			a.sel--
		case "pgdown":
			a.sel += 10
		case "pgup":
			a.sel -= 10
		case "home", "g":
			a.sel = 0
		case "end", "G":
			a.sel = 1 << 30
		}
		if a.sel < 0 {
			a.sel = 0
		}
	}
	return a, nil
}

var rigCols = []column{
	{title: "rig", width: 7, prio: 0},
	{title: "worker", width: 20, prio: 0},
	{title: "agent", width: 16, prio: 3},
	{title: "1m", width: 10, right: true, prio: 0},
	{title: "1h", width: 10, right: true, prio: 2},
	{title: "diff", width: 7, right: true, prio: 1},
	{title: "last share", width: 10, right: true, prio: 0},
	{title: "state", width: 8, prio: 0},
}

var submitCols = []column{
	{title: "time", width: 8, prio: 0},
	{title: "worker", width: 18, prio: 0},
	{title: "result", width: 16, prio: 0},
	{title: "diff", width: 8, right: true, prio: 0},
	{title: "sdiff", width: 8, right: true, prio: 1},
	{title: "of net", width: 10, right: true, prio: 2},
	{title: "agent", width: 16, prio: 3},
}

func (a *addressView) Render(ctx *Context, w, h int) []string {
	t := ctx.Theme
	s := ctx.Snap
	var data AddressData
	src, hasSrc := ctx.Provider.(AddressSource)
	if hasSrc {
		data = src.Address(a.addr)
	}
	var ws []WorkerRow
	var rate, best float64
	for _, wr := range s.Workers {
		if sameAddr(wr.Address, a.addr) {
			if ts, ok := data.LastShare[wr.Name]; ok && ts.After(wr.LastShare) {
				wr.LastShare = ts
			}
			ws = append(ws, wr)
			rate += wr.Hashrate1m
			best = max(best, wr.BestShare)
		}
	}
	nd := s.Pool.NetworkDiff
	sep := " " + ctx.sym.Sep + " "

	out := []string{
		" " + t.Title.Render("Address") + " " + t.Text.Render(a.addr),
		" " + t.Dim.Render(itoa(len(ws))+" workers"+sep) + t.Text.Render(fmtHash(rate)) + t.Dim.Render(sep+"best ") + t.Text.Render(fmtDiff(best)),
		"",
	}

	mine := dash
	if data.LogShares && nd > 0 {
		mine = fmtPct(data.Work / nd * 100)
	}
	ht := dash
	if data.Height > 0 {
		ht = strconv.FormatInt(data.Height, 10)
	}
	out = append(out,
		" "+kv(ctx, "since last network block", 33, mine)+t.Dim.Render("  work of this address at height "+ht+", resets at each new block"),
		" "+kv(ctx, "pool round since our last block", 33, fmtPct(PoolLuck(s.Pool.Accepted, nd)))+t.Dim.Render("  whole pool, resets only when the pool finds a block"),
		"",
	)

	// Rigs.
	out = append(out, " "+t.Title.Render("Rigs")+t.Dim.Render("   stale = no share for "+itoa(StaleK)+"x the expected interval"))
	if len(ws) == 0 {
		out = append(out, " "+t.Dim.Render("No connected workers for this address."))
	} else {
		keep := fitColumns(rigCols, w-1)
		out = append(out, " "+tableHead(ctx, rigCols, keep))
		for _, r := range sortedWorkers(ws, ctx.Now) {
			badge, tag := rigBadge(ctx, r)
			last := dash
			if !r.LastShare.IsZero() {
				last = fmtDur(ctx.Now.Sub(r.LastShare))
			}
			row := tableRow(rigCols, keep, []string{badge + " " + tag, rigName(r.Name), agentShort(r.UserAgent),
				ctx.hash(r.Hashrate1m), ctx.longHash(r, r.Hashrate1h), fmtDiff(r.Diff), last, stateLabel(ctx, ctx.classify(r))})
			if st := ctx.classify(r); st == StateOff || st == StateIdle {
				row = t.Dim.Render(row)
			}
			out = append(out, " "+row)
		}
	}
	out = append(out, "")

	// Live submits.
	out = append(out, " "+t.Title.Render("Live submits"))
	if !data.LogShares || len(data.Submits) == 0 {
		hint := "No sharelog lines for this address at the current height."
		if !data.LogShares {
			hint = "Sharelogs are off or unreadable: enable logshares in the pool configuration to see submits."
		}
		return append(out, " "+t.Dim.Render(hint))
	}
	keep := fitColumns(submitCols, w-1)
	out = append(out, " "+tableHead(ctx, submitCols, keep))
	room := h - len(out)
	n := len(data.Submits)
	hi := n - a.sel
	hi = max(min(hi, n), min(n, 1))
	lo := max(hi-room, 0)
	for i := hi - 1; i >= lo; i-- {
		sh := data.Submits[i]
		res := t.Good.Render("accepted")
		if !sh.Result {
			reason := sh.Error
			if reason == "" {
				reason = "rejected"
			}
			res = t.Bad.Render(reason)
		}
		pct := dash
		if nd > 0 {
			pct = fmtOfNet(sh.SDiff / nd * 100)
		}
		cells := []string{shareTime(sh), rigName(sh.WorkerName), res,
			fmtDiff(sh.Diff), fmtDiff(sh.SDiff), pct, agentShort(sh.Agent)}
		out = append(out, " "+tableRowStyled(keep, cells))
	}
	return out
}

// tableRowStyled lays out submit cells; the result cell may carry styling,
// so it is padded by visible width.
func tableRowStyled(keep []bool, cells []string) string {
	var b strings.Builder
	for i, c := range submitCols {
		if !keep[i] {
			continue
		}
		if c.right {
			b.WriteString(right(cells[i], c.width))
		} else {
			b.WriteString(fit(cells[i], c.width))
		}
		b.WriteByte(' ')
	}
	return b.String()
}

// rigName is the part of a worker name after the address.
func rigName(n string) string {
	if i := strings.LastIndexByte(n, '.'); i >= 0 {
		return n[i+1:]
	}
	return n
}

// shareTime formats a sharelog createdate ("<sec>,<usec>") as HH:MM:SS UTC.
func shareTime(s poolfmt.Share) string {
	sec, _, _ := strings.Cut(s.CreateDate, ",")
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return dash
	}
	return time.Unix(n, 0).UTC().Format("15:04:05")
}
