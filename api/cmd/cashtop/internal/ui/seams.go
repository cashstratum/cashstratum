package ui

import (
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The views below are replaceable from other files of this package: assign
// the variable (or register an overlay) in an init function.

// Overlay takes over the whole screen while it is active, e.g. a block
// celebration. Every message reaches Update first, including TickMsg and
// BlockFoundMsg; while Active, keys go only to the overlay (ctrl+c still
// quits).
type Overlay interface {
	Update(msg tea.Msg, ctx *Context) tea.Cmd
	Active() bool
	Render(ctx *Context, width, height int) string
}

var overlayFactories []func(*Context) Overlay

// registerOverlay adds an overlay constructor; every new Model gets one.
func registerOverlay(f func(*Context) Overlay) { overlayFactories = append(overlayFactories, f) }

// Page is a full-body view pushed over the tabs (sharelog drill-down,
// address view). Returning a nil Page from Update closes it; esc closes it
// too.
type Page interface {
	Title() string
	Update(msg tea.Msg, ctx *Context) (Page, tea.Cmd)
	Render(ctx *Context, width, height int) []string
}

// blockMeter draws the Block Meter on the dashboard: exactly two lines of
// width cells.
var blockMeter = placeholderMeter

// effortMeter draws the round effort meter under the best-share meter:
// exactly two lines of width cells.
var effortMeter = func(*Context, int) []string { return nil }

// sparkline draws a hashrate series in width cells.
var sparkline = placeholderSparkline

// shareRain, when set, replaces the Feed body while the rain toggle is on.
var shareRain func(ctx *Context, width, height int) []string

// addressPage opens the view for one payout address.
var addressPage = func(addr string) Page { return &addressSummary{addr: addr} }

// placeholderMeter is a plain log-scale bar of the best share since the last
// block against the network difficulty.
func placeholderMeter(ctx *Context, width int) []string {
	p := ctx.Snap.Pool
	frac := 0.0
	if p.BestShare > 1 && p.NetworkDiff > 1 {
		frac = math.Log10(p.BestShare) / math.Log10(p.NetworkDiff)
	}
	frac = math.Max(0, math.Min(1, frac))
	label := " best " + fmtDiff(p.BestShare) + " of " + fmtDiff(p.NetworkDiff)
	if p.NetworkDiff > 0 {
		label += " (" + fmtPct(p.BestShare/p.NetworkDiff*100) + ")"
	}
	bar := width - len(label) - 1
	if bar < 10 {
		bar = 10
	}
	n := int(frac*float64(bar) + 0.5)
	s := ctx.sym
	line := ctx.Theme.Accent.Render(strings.Repeat(s.MeterFull, n)) +
		ctx.Theme.Dim.Render(strings.Repeat(s.MeterEmpty, bar-n)) + " " + ctx.Theme.Text.Render(label)
	return []string{fit(line, width), fit(ctx.Theme.Dim.Render("log scale, 1 to network difficulty"), width)}
}

// placeholderSparkline maps the last width points onto the spark ramp.
func placeholderSparkline(ctx *Context, series []float64, width int) string {
	if len(series) == 0 || width <= 0 {
		return ""
	}
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
	return ctx.Theme.Accent.Render(b.String())
}
