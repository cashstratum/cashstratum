package ui

import (
	"strings"
	"time"
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
