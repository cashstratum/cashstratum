package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// box draws lines inside a titled border of exactly w x h cells.
func box(ctx *Context, title string, lines []string, w, h int) []string {
	if w < 4 || h < 2 {
		return nil
	}
	b := ctx.Caps.Box()
	t := ctx.Theme
	inner := w - 2
	out := make([]string, 0, h)
	out = append(out, t.Dim.Render(b.TL)+styledHead(ctx, title, b.H, inner)+t.Dim.Render(b.TR))
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, t.Dim.Render(b.V)+fit(l, inner)+t.Dim.Render(b.V))
	}
	out = append(out, t.Dim.Render(b.BL+strings.Repeat(b.H, inner)+b.BR))
	return out
}

func styledHead(ctx *Context, title, h string, inner int) string {
	t := ctx.Theme
	if title == "" {
		return t.Dim.Render(strings.Repeat(h, inner))
	}
	label := " " + title + " "
	lw := ansi.StringWidth(label)
	if lw+1 > inner {
		return t.Dim.Render(strings.Repeat(h, inner))
	}
	return t.Dim.Render(h) + t.Title.Render(label) + t.Dim.Render(strings.Repeat(h, inner-1-lw))
}

// hjoin places blocks side by side with a one-cell gap. Each block's lines
// must already be of equal width.
func hjoin(blocks ...[]string) []string {
	n := 0
	widths := make([]int, len(blocks))
	for i, b := range blocks {
		n = max(n, len(b))
		if len(b) > 0 {
			widths[i] = ansi.StringWidth(b[0])
		}
	}
	out := make([]string, n)
	for r := 0; r < n; r++ {
		var sb strings.Builder
		for i, b := range blocks {
			if i > 0 {
				sb.WriteByte(' ')
			}
			if r < len(b) {
				sb.WriteString(b[r])
			} else {
				sb.WriteString(strings.Repeat(" ", widths[i]))
			}
		}
		out[r] = sb.String()
	}
	return out
}

// kv formats a label and value pair with the label in the dim style.
func kv(ctx *Context, label string, lw int, value string) string {
	return ctx.Theme.Dim.Render(fit(label, lw)) + ctx.Theme.Text.Render(value)
}

// column is one column of a table.
type column struct {
	title string
	width int
	right bool
	prio  int // lower is kept first when the table is narrow
	flex  int // when > 0, the widest this column may grow to with spare room
}

// growFlex returns cols with the kept flex column widened to absorb the room
// left over in w, up to its flex maximum. Columns without a flex maximum, and
// every width when nothing is spare, come back unchanged.
func growFlex(cols []column, keep []bool, w int) []column {
	used := 0
	for i, c := range cols {
		if keep[i] {
			used += c.width + 1
		}
	}
	spare := w - used
	if spare <= 0 {
		return cols
	}
	out := make([]column, len(cols))
	copy(out, cols)
	for i, c := range out {
		if keep[i] && c.flex > c.width {
			out[i].width += min(spare, c.flex-c.width)
			break
		}
	}
	return out
}

// fitColumns drops the lowest-priority columns until the table fits w.
func fitColumns(cols []column, w int) []bool {
	keep := make([]bool, len(cols))
	for i := range keep {
		keep[i] = true
	}
	total := func() int {
		t := 0
		for i, c := range cols {
			if keep[i] {
				t += c.width + 1
			}
		}
		return t
	}
	for total() > w {
		worst := -1
		for i, c := range cols {
			if keep[i] && (worst < 0 || c.prio > cols[worst].prio) {
				worst = i
			}
		}
		if worst < 0 || cols[worst].prio == 0 {
			break
		}
		keep[worst] = false
	}
	return keep
}

func tableRow(cols []column, keep []bool, cells []string) string {
	var b strings.Builder
	for i, c := range cols {
		if !keep[i] {
			continue
		}
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		if c.right {
			b.WriteString(right(v, c.width))
		} else {
			b.WriteString(fit(v, c.width))
		}
		b.WriteByte(' ')
	}
	return b.String()
}

func tableHead(ctx *Context, cols []column, keep []bool) string {
	cells := make([]string, len(cols))
	for i, c := range cols {
		cells[i] = c.title
	}
	return ctx.Theme.Dim.Render(tableRow(cols, keep, cells))
}

// window returns the [lo, hi) slice of n rows of height h that keeps sel
// visible.
func window(n, h, sel int) (int, int) {
	if h <= 0 || n <= 0 {
		return 0, 0
	}
	lo := 0
	if sel >= h {
		lo = sel - h + 1
	}
	hi := min(n, lo+h)
	return lo, hi
}
