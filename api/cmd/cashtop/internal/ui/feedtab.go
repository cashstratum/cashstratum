package ui

func renderFeed(ctx *Context, w, h, offset int) []string {
	t := ctx.Theme
	rows := ctx.Snap.Feed
	state := t.Good.Render("following")
	if offset > 0 {
		state = t.Warn.Render("paused, " + itoa(offset) + " newer")
	}
	out := []string{" " + t.Title.Render("Feed") + "  " + state +
		t.Dim.Render("   "+itoa(len(rows))+" events "+ctx.sym.Sep+" r share rain"), ""}
	if len(rows) == 0 {
		return append(out, " "+t.Dim.Render("No events yet. New pool log lines appear here as they are written."))
	}
	room := h - len(out)
	end := len(rows) - offset
	start := max(end-room, 0)
	for _, r := range rows[start:end] {
		ts := r.Time.UTC().Format("15:04:05")
		tag := fit(r.Style, 6)
		out = append(out, " "+t.Dim.Render(ts)+" "+t.palette(r.Colour).Render(tag)+" "+t.Text.Render(r.Text))
	}
	return out
}
