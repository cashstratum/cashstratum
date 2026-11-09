package ui

import (
	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui/glyphs"
)

// helpLines is the whole help text; the screen scrolls when it does not fit.
func helpLines(ctx *Context) []string {
	t := ctx.Theme
	k := func(key, what string) string { return t.Accent.Render(fit(key, 14)) + t.Text.Render(what) }
	lines := helpBase(ctx, t, k)
	if !ctx.Snap.Prop.On {
		return lines
	}
	// A pro-rata pool gets its own section at the top: it is the view the
	// operator opened the tool for.
	return append([]string{
		t.Title.Render("PROP view"),
		k("est. reward", "an address's share of the miners' pot (48% of the coinbase), in BCH, an estimate"),
		k("", "until the block confirms; under 1% of the round is unpaid and shared by the rest"),
		k("finder bonus", "the finder's 50% output; it is not part of the estimates"),
		"",
	}, lines...)
}

func helpBase(ctx *Context, t Theme, k func(key, what string) string) []string {
	return []string{
		t.Title.Render("Keys"),
		k("1 2 3 4 5", "Dashboard, Workers, Blocks, Feed, Network"),
		k("tab / h l", "next / previous tab"),
		k("j k / arrows", "move selection, scroll (also scrolls this help)"),
		k("g G", "first / last (Feed: G follows new events)"),
		k("enter", "Dashboard: miner view of the picked round row; Workers: address view;"),
		k("", "Blocks: sharelog jobs for the height"),
		k("o", "Workers: show or hide workers that are off"),
		k("/", "search: a height, an address or a worker name"),
		k(":", "command: test-celebrate replays the newest block, q quits"),
		k("esc", "close search, help or a detail view"),
		k("esc enter spc", "dismiss the block-found banner"),
		k("r", "Feed: toggle share rain"),
		k("ctrl+l", "redraw the whole screen after the terminal got garbled"),
		k("?", "this help"),
		k("q  ctrl+c", "quit"),
		"",
		t.Title.Render("Pool box"),
		k("workers", "live, stale, idle and off workers, classified by the table below"),
		k("reported", "conn: clients; idle: >60s no share; resum: dropped, kept 600s"),
		k("", "daemon counts; not comparable to live/stale/off"),
		k("rejected", "rejected / (accepted + rejected) share difficulty"),
		k("last pool block", "time since the pool last found a block"),
		k("top round", "the address with the biggest share of the pool round since the last block"),
		"",
		t.Title.Render("Effort"),
		k("round effort", "accepted share difficulty / network difficulty x 100"),
		k("pool effort", "the whole pool's work since the pool's previous block / network difficulty x 100"),
		k("finder effort", "the finder's own work since its previous block / network difficulty x 100;"),
		k("", "each miner's round restarts at its own blocks, so it can far exceed pool effort;"),
		k("", dash+" when unknown"),
		k("P(block t)", "1 - exp(-H t / (D 2^32)), H pool hashrate, D difficulty"),
		k("expected", "D 2^32 / H"),
		k("pool rate", "1h, falling back to 15m, 5m, 1m, 1d when empty"),
		k("pool share", "pool rate / (D 2^32 / 600); x 144 = expected blocks/day"),
		"",
		t.Title.Render("Workers"),
		k("live", "recent shares at the expected rate"),
		k("stale", "no share for "+itoa(StaleK)+"x the expected interval at the worker's difficulty (min 1m)"),
		k("idle", "connected, no share for a while: rates decayed to 0"),
		k("off", "not connected; hidden until o. Reads \"off <ago> "+ctx.sym.Sep+" <reason>\" when it dropped"),
		k("", "peer_eof: the miner closed the connection; peer_reset: connection reset"),
		k("", "(network or device); invalid_shares: the pool dropped it for bad shares"),
		k("rig badges", glyphLegend(ctx)),
		"",
		t.Title.Render("Share rain"),
		k("lanes", "one lane per miner; shares fall as they are submitted"),
		k("glyphs", rainHelpLegend(ctx)),
		"",
		t.Title.Render("Network"),
		k("recent blocks", "latest network blocks with the pool that found them, from the coinbase"),
		k("", "tag; "+dash+" is an unknown pool; our own finds are marked"),
		k("net block", "amber "+itoa(int(NetBlockAmber.Minutes()))+"m, red "+itoa(int(NetBlockRed.Minutes()))+"m: our chain view may be stale"),
		k("zmq", "off: not configured; down: endpoint unreachable; waiting: connected, no block yet"),
		k("Feed", "USER/WORKER lines: 1m 5m 1h decayed hashrates (keep decaying after a"),
		k("", "worker leaves), <n>w live connections, best round best, luck, rej rejects"),
	}
}

func renderHelp(ctx *Context, w, h int) []string { return renderHelpAt(ctx, w, h, 0) }

// renderHelpAt draws the help scrolled down by off rows, clamped so the last
// page stays full.
func renderHelpAt(ctx *Context, w, h, off int) []string {
	lines := helpLines(ctx)
	inner := max(h-3, 1) // blank row above, box borders
	off = max(min(off, len(lines)-inner), 0)
	title := "Help"
	if len(lines) > inner {
		title += " " + itoa(off+1) + "-" + itoa(min(off+inner, len(lines))) + "/" + itoa(len(lines)) + " (j/k scroll)"
		lines = lines[off:min(off+inner, len(lines))]
	}
	bw := min(w, 98)
	b := box(ctx, title, lines, bw, min(h-1, len(lines)+2))
	pad := (w - bw) / 2
	out := make([]string, 0, len(b)+1)
	out = append(out, "")
	for _, l := range b {
		out = append(out, fit("", pad)+l)
	}
	return out
}

func rainHelpLegend(ctx *Context) string {
	g := func(k rainKind, what string) string { return rainStyle(ctx, k)(rainGlyph(ctx, k)) + " " + what }
	return g(rainAccepted, "accepted  ") + g(rainRejected, "rejected  ") + g(rainNear, "near miss (>=1% of net)")
}

func glyphLegend(ctx *Context) string {
	ascii := ctx.Caps.Glyphs == termcap.ASCII
	owned, other := glyphs.Badge(glyphs.ASIC, ascii), glyphs.Badge(glyphs.Unknown, ascii)
	return owned + " own hardware (asic, axe)  " + other + " rental, proxy, software or unknown"
}
