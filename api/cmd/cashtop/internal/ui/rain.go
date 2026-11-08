package ui

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func init() { shareRain = renderShareRain }

type rainKind int

const (
	rainAccepted rainKind = iota
	rainRejected
	rainNear
)

// maxRainDrops bounds how many shares are drawn per frame, so a large rig
// cannot flood the screen or the renderer.
const maxRainDrops = 240

// rainDrop is one drawable share.
type rainDrop struct {
	kind rainKind
	time time.Time
	text string
}

// shareKindOf classifies a submitted share.
func shareKindOf(d ShareDrop) rainKind {
	switch {
	case !d.Accepted:
		return rainRejected
	case d.Near:
		return rainNear
	}
	return rainAccepted
}

// rainDrops reads Snapshot.Shares, falling back to feed rows when no
// sharelog data is available.
func rainDrops(s *Snapshot) []rainDrop {
	if len(s.Shares) > 0 {
		out := make([]rainDrop, len(s.Shares))
		for i, d := range s.Shares {
			out[i] = rainDrop{shareKindOf(d), d.Time, d.Worker + "  diff " + fmtDiff(d.Diff) + "  sdiff " + fmtDiff(d.SDiff)}
		}
		return out
	}
	out := make([]rainDrop, len(s.Feed))
	for i, r := range s.Feed {
		out[i] = rainDrop{rainKindOf(r), r.Time, r.Text}
	}
	return out
}

func rainKindOf(r FeedRow) rainKind {
	switch strings.ToUpper(strings.TrimSpace(r.Style)) {
	case "BLOCK", "BEST", "NEAR":
		return rainNear
	case "DROP", "ERR", "ERROR", "REJ", "REJECT", "BAD", "FLAP":
		return rainRejected
	}
	return rainAccepted
}

func rainGlyph(ctx *Context, k rainKind) string {
	ascii := !ctx.Caps.BrailleOK()
	switch k {
	case rainRejected:
		if ascii {
			return "x"
		}
		return "×"
	case rainNear:
		if ascii {
			return "*"
		}
		return "◆"
	}
	if ascii {
		return "."
	}
	return ctx.sym.Bullet
}

func rainStyle(ctx *Context, k rainKind) func(...string) string {
	t := ctx.Theme
	switch k {
	case rainRejected:
		return t.Bad.Render
	case rainNear:
		return t.Gold.Render
	}
	return t.Good.Render
}

// rainSample keeps at most limit of the newest shares, evenly thinned.
func rainSample[T any](rows []T, limit int) []T {
	if len(rows) <= limit {
		return rows
	}
	out := make([]T, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, rows[len(rows)-limit+i])
	}
	return out
}

func rainHash(i int) uint32 {
	x := uint32(i)*2654435761 + 40503
	x ^= x >> 15
	x *= 2246822519
	x ^= x >> 13
	return x
}

func renderShareRain(ctx *Context, w, h int) []string {
	if len(ctx.Snap.Shares) > 0 {
		return renderLaneRain(ctx, w, h)
	}
	t := ctx.Theme
	all := rainDrops(ctx.Snap)
	unit := " shares "
	if len(ctx.Snap.Shares) == 0 {
		unit = " events "
	}
	head := " " + t.Title.Render("Share rain") + t.Dim.Render("   "+itoa(len(all))+unit+ctx.sym.Sep+" r back to feed")
	room := h - 2
	if room < 1 {
		return []string{head}
	}
	if !ctx.Motion || !ctx.Caps.BrailleOK() {
		return append([]string{head, ""}, staticRain(ctx, all, room)...)
	}
	cols := (w - 2) / 2
	if cols < 1 {
		return []string{head}
	}
	type cell struct {
		g  string
		st func(...string) string
	}
	grid := make([][]cell, room)
	for i := range grid {
		grid[i] = make([]cell, cols)
	}
	drops := rainSample(all, maxRainDrops)
	for j, r := range drops {
		hv := rainHash(j)
		c := int(hv % uint32(cols))
		y := (int((hv>>8)%uint32(room)) + ctx.Frame) % room
		g := rainGlyph(ctx, r.kind)
		grid[y][c] = cell{g, rainStyle(ctx, r.kind)}
		if ty := (y + room - 1) % room; grid[ty][c].g == "" {
			grid[ty][c] = cell{g, t.Dim.Render}
		}
	}
	out := []string{head, ""}
	for _, row := range grid {
		var b strings.Builder
		b.WriteString(" ")
		for _, c := range row {
			if c.g == "" {
				b.WriteString("  ")
				continue
			}
			b.WriteString(c.st(c.g) + " ")
		}
		out = append(out, fit(b.String(), w))
	}
	return out
}

func staticRain(ctx *Context, rows []rainDrop, room int) []string {
	t := ctx.Theme
	if len(rows) == 0 {
		return []string{" " + t.Dim.Render("No shares yet.")}
	}
	start := max(len(rows)-room, 0)
	var out []string
	for _, r := range rows[start:] {
		g := rainGlyph(ctx, r.kind)
		out = append(out, " "+rainStyle(ctx, r.kind)(g)+" "+t.Dim.Render(r.time.UTC().Format("15:04:05"))+" "+t.Text.Render(r.text))
	}
	return out
}

// minLaneWidth is the narrowest lane that still fits a short address label
// and a few glyph slots.
const minLaneWidth = 9

// maxLaneWidth keeps a lone lane from stretching across the whole screen.
const maxLaneWidth = 18

// rainLane is one address's column of drops. The final lane may be the
// aggregate of every address that did not get a column of its own.
type rainLane struct {
	addr   string
	others bool
	drops  []rainKind
	pct    float64
	pctOK  bool
}

// workerAddress reduces a "<address>.<rig>" worker name to its bare payout
// address, without the CashAddr prefix.
func workerAddress(worker string) string {
	addr, _, _ := strings.Cut(worker, ".")
	return strings.TrimPrefix(addr, "bitcoincash:")
}

// roundPct returns an address's share of the current round, in percent.
func roundPct(r RoundInfo, addr string) (float64, bool) {
	for _, m := range r.Miners {
		if strings.TrimPrefix(m.Address, "bitcoincash:") == addr {
			return m.Pct, true
		}
	}
	return 0, false
}

// buildLanes groups shares by address. Lanes follow the round order when
// Round.Miners is known; addresses missing from it, and every address when it
// is empty, follow by share count, then address; when more addresses exist than maxLanes, the tail collapses into a
// single trailing "others" lane. Drops keep their arrival order.
func buildLanes(s *Snapshot, maxLanes int) []rainLane {
	maxLanes = max(maxLanes, 1)
	byAddr := map[string][]rainKind{}
	for _, d := range s.Shares {
		a := workerAddress(d.Worker)
		byAddr[a] = append(byAddr[a], shareKindOf(d))
	}
	addrs := make([]string, 0, len(byAddr))
	for a := range byAddr {
		addrs = append(addrs, a)
	}
	rank := map[string]int{}
	for i, m := range s.Round.Miners {
		a := strings.TrimPrefix(m.Address, "bitcoincash:")
		if _, dup := rank[a]; !dup {
			rank[a] = i
		}
	}
	sort.Slice(addrs, func(i, j int) bool {
		ri, iIn := rank[addrs[i]]
		rj, jIn := rank[addrs[j]]
		if iIn != jIn {
			return iIn
		}
		if iIn && ri != rj {
			return ri < rj
		}
		ci, cj := len(byAddr[addrs[i]]), len(byAddr[addrs[j]])
		if ci != cj {
			return ci > cj
		}
		return addrs[i] < addrs[j]
	})
	keep := addrs
	var rest []string
	if len(addrs) > maxLanes {
		keep, rest = addrs[:maxLanes-1], addrs[maxLanes-1:]
	}
	roundKnown := len(s.Round.Miners) > 0
	lanes := make([]rainLane, 0, maxLanes)
	shown := 0.0
	for _, a := range keep {
		l := rainLane{addr: a, drops: byAddr[a]}
		l.pct, l.pctOK = roundPct(s.Round, a)
		shown += l.pct
		lanes = append(lanes, l)
	}
	if len(rest) > 0 {
		l := rainLane{others: true}
		for _, a := range rest {
			l.drops = append(l.drops, byAddr[a]...)
		}
		if roundKnown {
			l.pct, l.pctOK = max(100-shown, 0), true
		}
		lanes = append(lanes, l)
	}
	return lanes
}

func laneLabel(ctx *Context, l rainLane, w int) string {
	if l.others {
		return "others"
	}
	return shortHash(l.addr, max((w-3)/2, 2), ctx.sym.Ellipsis)
}

func center(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return fit(s, w)
	}
	l := (w - sw) / 2
	return strings.Repeat(" ", l) + s + strings.Repeat(" ", w-sw-l)
}

func rainLegend(ctx *Context) string {
	t := ctx.Theme
	return " " + rainStyle(ctx, rainAccepted)(rainGlyph(ctx, rainAccepted)) + t.Dim.Render(" accepted  ") +
		rainStyle(ctx, rainRejected)(rainGlyph(ctx, rainRejected)) + t.Dim.Render(" rejected  ") +
		rainStyle(ctx, rainNear)(rainGlyph(ctx, rainNear)) + t.Dim.Render(" near miss")
}

// renderLaneRain draws one column of falling shares per address.
func renderLaneRain(ctx *Context, w, h int) []string {
	t := ctx.Theme
	head := rainHeader(ctx, w)
	legend := fit(rainLegend(ctx), w)
	if !ctx.Motion || !ctx.Caps.BrailleOK() {
		room := h - 3
		if room < 1 {
			return []string{head}
		}
		return append([]string{head, legend, ""}, staticRain(ctx, rainDrops(ctx.Snap), room)...)
	}
	room := h - 5 // head, labels, pct row, legend, blank
	n := (w - 1) / minLaneWidth
	if room < 1 || n < 1 {
		return []string{head}
	}
	lanes := buildLanes(ctx.Snap, n)
	lw := min((w-1)/len(lanes), maxLaneWidth)
	slots := max((lw-2)/2, 1)
	type cell struct {
		g  string
		st func(...string) string
	}
	grid := make([][]cell, room)
	for i := range grid {
		grid[i] = make([]cell, len(lanes)*lw)
	}
	limit := max(maxRainDrops/len(lanes), 1)
	for li, l := range lanes {
		for j, k := range rainSample(l.drops, limit) {
			hv := rainHash(j + li*977)
			c := li*lw + 1 + 2*int(hv%uint32(slots))
			y := (int((hv>>8)%uint32(room)) + ctx.Frame) % room
			g := rainGlyph(ctx, k)
			grid[y][c] = cell{g, rainStyle(ctx, k)}
			if ty := (y + room - 1) % room; grid[ty][c].g == "" {
				grid[ty][c] = cell{g, t.Dim.Render}
			}
		}
	}
	labels, pcts := " ", " "
	for _, l := range lanes {
		labels += t.Title.Render(center(laneLabel(ctx, l, lw), lw))
		p := dash
		if l.pctOK {
			p = fmtPct(l.pct)
		}
		pcts += t.Dim.Render(center(p, lw))
	}
	out := []string{head, fit(labels, w)}
	for _, row := range grid {
		var b strings.Builder
		b.WriteString(" ")
		for _, c := range row {
			if c.g == "" {
				b.WriteString(" ")
				continue
			}
			b.WriteString(c.st(c.g))
		}
		out = append(out, fit(b.String(), w))
	}
	return append(out, fit(pcts, w), "", legend)
}

// rainHeader builds the title line, dropping trailing parts whole when the
// width is short, never cutting one mid-word.
func rainHeader(ctx *Context, w int) string {
	t := ctx.Theme
	head := " " + t.Title.Render("Share rain")
	for _, part := range []string{
		itoa(len(ctx.Snap.Shares)) + " shares",
		"lane=address",
		"r back",
	} {
		next := head + t.Dim.Render(" "+ctx.sym.Sep+" "+part)
		if ansi.StringWidth(next) > w {
			break
		}
		head = next
	}
	return fit(head, w)
}
