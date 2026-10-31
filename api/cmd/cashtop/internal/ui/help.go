package ui

import (
	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui/glyphs"
)

func renderHelp(ctx *Context, w, h int) []string {
	t := ctx.Theme
	k := func(key, what string) string { return t.Accent.Render(fit(key, 14)) + t.Text.Render(what) }
	lines := []string{
		k("1 2 3 4 5", "Dashboard, Workers, Blocks, Feed, Network"),
		k("tab / h l", "next / previous tab"),
		k("j k / arrows", "move selection, scroll"),
		k("g G", "first / last (Feed: G follows new events)"),
		k("enter", "Blocks: sharelog jobs for the height; Workers: address view"),
		k("/", "search: a height, an address or a worker name"),
		k(":", "command: test-celebrate replays the newest block, q quits"),
		k("esc", "close search, help or a detail view"),
		k("r", "Feed: toggle share rain"),
		k("?", "this help"),
		k("q  ctrl+c", "quit"),
		"",
		t.Title.Render("Numbers"),
		k("round effort", "accepted share difficulty / network difficulty x 100"),
		k("P(block t)", "1 - exp(-H t / (D 2^32)), H pool hashrate, D difficulty"),
		k("expected", "D 2^32 / H"),
		k("stale", "no share for "+itoa(StaleK)+"x the expected interval at the worker's difficulty (min 1m)"),
		k("rig badges", glyphLegend(ctx)),
	}
	bw := min(w, 96)
	b := box(ctx, "Help", lines, bw, min(h, len(lines)+2))
	pad := (w - bw) / 2
	out := make([]string, 0, len(b)+1)
	out = append(out, "")
	for _, l := range b {
		out = append(out, fit("", pad)+l)
	}
	return out
}

func glyphLegend(ctx *Context) string {
	ascii := ctx.Caps.Glyphs == termcap.ASCII
	owned, other := glyphs.Badge(glyphs.ASIC, ascii), glyphs.Badge(glyphs.Unknown, ascii)
	return owned + " own hardware (asic, axe)  " + other + " rental, proxy, software or unknown"
}
