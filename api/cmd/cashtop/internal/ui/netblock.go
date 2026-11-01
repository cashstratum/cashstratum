package ui

import (
	"math"
	"strconv"
	"time"
)

// Thresholds and durations of the network-block indicators.
const (
	// NetBlockAmber and NetBlockRed colour the time since the last network
	// block: normal below the first, amber from it, red from the second. The
	// colour means "our view of the chain may be stale", not "slow block":
	// with 10-minute exponential intervals a gap of 20 minutes (e^-2) occurs
	// after 13.5% of blocks and 40 minutes (e^-4) after 1.8%, so red is rare
	// enough to be worth a look.
	NetBlockAmber = 20 * time.Minute
	NetBlockRed   = 40 * time.Minute

	// NetBlockBannerFor is how long the new-block banner stays up, and
	// NetBlockFlashFor how long the dashboard line is highlighted.
	NetBlockBannerFor = 5 * time.Second
	NetBlockFlashFor  = time.Second
)

// AgeLevel grades the time since the last network block.
type AgeLevel int

const (
	AgeNormal AgeLevel = iota
	AgeAmber
	AgeRed
)

// NetBlockAgeLevel grades age against the amber and red thresholds.
func NetBlockAgeLevel(age time.Duration) AgeLevel {
	switch {
	case age >= NetBlockRed:
		return AgeRed
	case age >= NetBlockAmber:
		return AgeAmber
	}
	return AgeNormal
}

// DiffDir is the direction of a difficulty change.
type DiffDir int

const (
	DiffSame DiffDir = iota
	DiffUp
	DiffDown
)

// DiffChange is the percent change from prev to cur. ok is false when either
// is unknown. A change that rounds to 0.00% has direction DiffSame.
func DiffChange(prev, cur float64) (pct float64, dir DiffDir, ok bool) {
	if prev <= 0 || cur <= 0 || math.IsNaN(prev) || math.IsNaN(cur) || math.IsInf(prev, 0) || math.IsInf(cur, 0) {
		return 0, DiffSame, false
	}
	pct = (cur - prev) / prev * 100
	switch {
	case math.Abs(pct) < 0.005:
		return 0, DiffSame, true
	case pct > 0:
		return pct, DiffUp, true
	}
	return pct, DiffDown, true
}

// fmtChangePct renders the magnitude of a change with the given decimals.
func fmtChangePct(pct float64, decimals int) string {
	return strconv.FormatFloat(math.Abs(pct), 'f', decimals, 64) + "%"
}

// diffChangeText is the plain text of a difficulty with its change.
func diffChangeText(sym symbols, cur, prev float64) string {
	if cur <= 0 {
		return dash
	}
	pct, dir, ok := DiffChange(prev, cur)
	if !ok {
		return fmtDiff(cur)
	}
	return fmtDiff(cur) + " " + arrow(sym, dir) + fmtChangePct(pct, 2)
}

func arrow(sym symbols, d DiffDir) string {
	switch d {
	case DiffUp:
		return sym.Up + " "
	case DiffDown:
		return sym.Down + " "
	}
	return ""
}

// diffChangeStyled colours the change: up amber, down green.
func diffChangeStyled(ctx *Context, cur, prev float64) string {
	if cur <= 0 {
		return dash
	}
	pct, dir, ok := DiffChange(prev, cur)
	if !ok {
		return fmtDiff(cur)
	}
	chg := arrow(ctx.sym, dir) + fmtChangePct(pct, 2)
	switch dir {
	case DiffUp:
		chg = ctx.Theme.Warn.Render(chg)
	case DiffDown:
		chg = ctx.Theme.Good.Render(chg)
	}
	return fmtDiff(cur) + " " + chg
}

// netBlockAge renders the time since the last network block, coloured by
// its level; a dash when unknown. It follows ctx.Now, so it ticks.
func netBlockAge(ctx *Context, last time.Time) string {
	if last.IsZero() {
		return dash
	}
	age := max(ctx.Now.Sub(last), 0)
	s := fmtDur(age) + " ago"
	switch NetBlockAgeLevel(age) {
	case AgeRed:
		return ctx.Theme.Bad.Render(s)
	case AgeAmber:
		return ctx.Theme.Warn.Render(s)
	}
	return s
}

// netBlockBanner is the short notice shown under the header after a new
// network block.
func netBlockBanner(ctx *Context, m NetworkBlockMsg, w int) string {
	sep := " " + ctx.sym.Sep + " "
	text := " NEW BLOCK"
	if m.Height > 0 {
		text += " " + itoa(m.Height)
	}
	if pct, dir, ok := DiffChange(m.PrevDiff, m.Diff); ok {
		text += sep + "diff " + arrow2(ctx.sym, dir) + fmtChangePct(pct, 1)
	}
	if m.Gap > 0 {
		text += sep + fmtDur(m.Gap) + " after previous"
	}
	return ctx.Theme.Title.Render(fit(text, w))
}

func arrow2(sym symbols, d DiffDir) string {
	switch d {
	case DiffUp:
		return sym.Up
	case DiffDown:
		return sym.Down
	}
	return ""
}
