package ui

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

func init() { sparkline = renderSparkline }

// Braille dot bits for a column filled from the bottom: left column dots
// 7,3,2,1 and right column dots 8,6,5,4.
var (
	brailleLeft  = [4]rune{0x40, 0x04, 0x02, 0x01}
	brailleRight = [4]rune{0x80, 0x20, 0x10, 0x08}
)

// packBraille builds one braille cell from two column heights (0 to 4 dots,
// filled from the bottom, so the tallest value reaches the top row).
func packBraille(l, r int) rune {
	bits := rune(0)
	for i := 0; i < clampInt(l, 0, 4); i++ {
		bits |= brailleLeft[i]
	}
	for i := 0; i < clampInt(r, 0, 4); i++ {
		bits |= brailleRight[i]
	}
	return 0x2800 + bits
}

func clampInt(v, lo, hi int) int { return max(lo, min(hi, v)) }

// levels scales a series to heights 1..4 between its minimum and maximum.
func levels(series []float64) []int {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range series {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	out := make([]int, len(series))
	for i, v := range series {
		f := 0.5
		if hi > lo {
			f = (v - lo) / (hi - lo)
		}
		out[i] = 1 + int(f*3+0.5)
	}
	return out
}

// breathe picks the style of the newest cell for a frame.
func breathe(ctx *Context) lipgloss.Style {
	a := ctx.Theme.Accent
	if !ctx.Motion {
		return a
	}
	switch ctx.Frame % 4 {
	case 0:
		return a.Faint(true)
	case 2:
		return a.Bold(true)
	}
	return a
}

func renderSparkline(ctx *Context, series []float64, width int) string {
	if len(series) == 0 || width <= 0 {
		return ""
	}
	if !ctx.Caps.BrailleOK() {
		return asciiSparkline(ctx, series, width)
	}
	if len(series) > 2*width {
		series = series[len(series)-2*width:]
	}
	lv := levels(series)
	if len(lv)%2 == 1 {
		lv = append(lv, lv[len(lv)-1])
	}
	cells := len(lv) / 2
	var body, head strings.Builder
	for i := 0; i < cells; i++ {
		r := string(packBraille(lv[2*i], lv[2*i+1]))
		if i == cells-1 {
			head.WriteString(breathe(ctx).Render(r))
		} else {
			body.WriteString(r)
		}
	}
	pad := strings.Repeat(" ", width-cells)
	return pad + ctx.Theme.Accent.Render(body.String()) + head.String()
}

func asciiSparkline(ctx *Context, series []float64, width int) string {
	if len(series) > width {
		series = series[len(series)-width:]
	}
	lo, hi := series[0], series[0]
	for _, v := range series {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	var b strings.Builder
	for _, v := range series {
		f := 0.5
		if hi > lo {
			f = (v - lo) / (hi - lo)
		}
		b.WriteRune(ctx.Caps.SparkLevel(f))
	}
	return strings.Repeat(" ", width-len(series)) + ctx.Theme.Accent.Render(b.String())
}
