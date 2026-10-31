package ui

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

func init() {
	blockMeter = renderBlockMeter
	effortMeter = renderEffortMeter
}

// meterPos maps a share difficulty onto a log scale from 1 to the network
// difficulty, returning a cell index in [0, cells-1]. ok is false when the
// value cannot be placed.
func meterPos(v, net float64, cells int) (int, bool) {
	if cells <= 0 || net <= 1 || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	f := math.Log10(math.Max(v, 1)) / math.Log10(net)
	f = math.Max(0, math.Min(1, f))
	return int(f*float64(cells-1) + 0.5), true
}

func renderBlockMeter(ctx *Context, width int) []string {
	p := ctx.Snap.Pool
	t, s := ctx.Theme, ctx.sym
	label := " best " + fmtDiff(p.BestShare)
	if p.NetworkDiff > 0 && p.BestShare > 0 {
		label += " (" + fmtPct(p.BestShare/p.NetworkDiff*100) + " of net)"
	}
	barW := width - len(label)
	if barW < 10 {
		barW = 10
	}
	fill := 0
	if i, ok := meterPos(p.BestShare, p.NetworkDiff, barW); ok {
		fill = i + 1
	}
	var b strings.Builder
	for i := 0; i < barW; i++ {
		switch {
		case i < fill:
			b.WriteString(t.Accent.Render(s.MeterFull))
		default:
			b.WriteString(t.Dim.Render(s.MeterEmpty))
		}
	}
	line1 := b.String() + t.Text.Render(label)

	legend := t.Dim.Render("1 " + s.Ellipsis + " " + fmtDiff(p.NetworkDiff) + "  log scale")
	return []string{fit(line1, width), fit(legend, width)}
}

// Round effort axis, in percent of the network difficulty: the bar is linear
// from 0 to effortMax, with a marker at effortDue.
const (
	effortDue = 100.0
	effortHot = 200.0
	effortMax = 300.0
)

// effortPos maps an effort percentage onto a linear 0..effortMax scale and
// returns a cell index in [0, cells-1], clipping values past the end.
func effortPos(pct float64, cells int) (int, bool) {
	if cells <= 0 || pct < 0 || math.IsNaN(pct) || math.IsInf(pct, 0) {
		return 0, false
	}
	f := math.Min(pct, effortMax) / effortMax
	return int(f*float64(cells-1) + 0.5), true
}

// effortTicks returns the cell of every past block at the effort it was found.
func effortTicks(blocks []BlockRow, cells int) []int {
	var out []int
	for _, b := range blocks {
		if b.Luck <= 0 {
			continue
		}
		if i, ok := effortPos(b.Luck, cells); ok {
			out = append(out, i)
		}
	}
	return out
}

// medianEffort is the median found effort of the past blocks; n is how many
// blocks contributed.
func medianEffort(blocks []BlockRow) (med float64, n int) {
	var v []float64
	for _, b := range blocks {
		if b.Luck > 0 {
			v = append(v, b.Luck)
		}
	}
	if len(v) == 0 {
		return 0, 0
	}
	sort.Float64s(v)
	if len(v)%2 == 1 {
		return v[len(v)/2], len(v)
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2, len(v)
}

// effortStyle picks the fill colour: accent below due, amber up to hot, red
// beyond.
func effortStyle(t Theme, pct float64) lipgloss.Style {
	switch {
	case pct > effortHot:
		return t.Bad
	case pct >= effortDue:
		return t.Warn
	}
	return t.Accent
}

func renderEffortMeter(ctx *Context, width int) []string {
	p := ctx.Snap.Pool
	t, s := ctx.Theme, ctx.sym
	stale := ctx.stale()
	effort := p.RoundPercent
	if effort <= 0 {
		effort = PoolLuck(p.Accepted, p.NetworkDiff)
	}
	med, n := medianEffort(ctx.Snap.Blocks)

	label := " effort " + fmtPct(effort)
	if stale {
		label = " effort " + dash
	} else if n > 0 {
		label += " " + s.Sep + " median of " + itoa(n) + " blocks " + fmtPct(med)
	}
	labelW := utf8.RuneCountInString(label)
	if cnt := len(effortTicks(ctx.Snap.Blocks, width)); cnt > 0 {
		labelW = max(labelW, len(" past blocks ("+itoa(cnt)+")"))
	}
	barW := width - labelW
	if barW < 10 {
		barW = 10
	}

	fill := 0
	if i, ok := effortPos(effort, barW); ok && !stale && effort > 0 {
		fill = i + 1
	}
	due, _ := effortPos(effortDue, barW)
	style := effortStyle(t, effort)
	var b strings.Builder
	for i := 0; i < barW; i++ {
		switch {
		case i == due:
			b.WriteString(t.Text.Render(s.MeterDue))
		case i < fill:
			b.WriteString(style.Render(s.MeterFull))
		default:
			b.WriteString(t.Dim.Render(s.MeterEmpty))
		}
	}
	lines := []string{fit(b.String()+t.Text.Render(label), width)}

	if cnt := len(effortTicks(ctx.Snap.Blocks, barW)); cnt > 0 {
		lines = append(lines, fit(rugLine(ctx, barW, cnt), width))
	}

	max := "300%"
	if !stale && effort > effortMax {
		max += "+"
	}
	lines = append(lines, fit(t.Dim.Render(scaleLegend(barW, due, max)), width))
	return lines
}

// rugLine draws one gold mark per past block on the bar's scale, denser where
// several blocks share a cell, followed by the row label.
func rugLine(ctx *Context, barW, count int) string {
	t, s := ctx.Theme, ctx.sym
	per := make([]int, barW)
	for _, i := range effortTicks(ctx.Snap.Blocks, barW) {
		per[i]++
	}
	var b strings.Builder
	for _, n := range per {
		switch {
		case n == 0:
			b.WriteString(t.Dim.Render(s.MeterRug))
		case n >= 3:
			b.WriteString(t.Gold.Render(s.MeterRamp[2]))
		default:
			b.WriteString(t.Gold.Render(s.MeterRamp[n-1]))
		}
	}
	return b.String() + t.Text.Render(" past blocks ("+itoa(count)+")")
}

// scaleLegend lays out 0, the due label and the scale end across barW cells,
// with the due label's percentage centred under the due marker. Labels that
// would collide are dropped.
func scaleLegend(barW, due int, max string) string {
	cells := []rune(strings.Repeat(" ", barW))
	put := func(text string, start int) {
		r := []rune(text)
		if start < 0 || start+len(r) > barW {
			return
		}
		for i := start; i < start+len(r)+1 && i < barW; i++ {
			if cells[i] != ' ' {
				return
			}
		}
		copy(cells[start:], r)
	}
	put(max, barW-len([]rune(max)))
	put("0", 0)
	put("100% due", due-1)
	return string(cells)
}
