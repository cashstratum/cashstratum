package ui

import "strconv"

// propRoundCols is the round table of a pro-rata pool: the effort column of
// the solo table gives way to the estimated reward and its eligibility.
var propRoundCols = []column{
	{title: "#", width: 3, prio: 0},
	{title: "address", width: 12, prio: 0, flex: 20},
	{title: "% round", width: 8, right: true, prio: 0},
	{title: "est. reward", width: 13, right: true, prio: 0},
	{title: "elig.", width: 5, prio: 1},
	{title: "best", width: 8, right: true, prio: 2},
	{title: "of net", width: 9, right: true, prio: 3},
}

// fmtEstBCH is an estimated reward in BCH to the satoshi.
func fmtEstBCH(sats int64) string {
	return strconv.FormatFloat(float64(sats)/1e8, 'f', 8, 64)
}

// estCell is the estimated reward of m, a dash while the template's coinbase
// value is unknown.
func estCell(p PropInfo, m RoundMiner) string {
	if p.Reward <= 0 {
		return dash
	}
	return fmtEstBCH(m.EstSats)
}

// eligCell marks an address under the eligibility threshold.
func eligCell(p PropInfo, m RoundMiner) string {
	switch {
	case p.Reward <= 0:
		return dash
	case m.Eligible:
		return "yes"
	}
	return "<1%"
}

// propRoundRow is the cells of one row of the pro-rata round table.
func propRoundRow(ctx *Context, i int, m RoundMiner) []string {
	p := ctx.Snap.Prop
	return []string{itoa(i + 1), shortAddr(m.Address, ctx.sym.Ellipsis), fmtPct(m.Pct),
		estCell(p, m), eligCell(p, m), fmtDiff(m.Best), fmtOfNet(m.BestOfNet)}
}

// propRoundLines replaces P(block 24h), P(block 7d) and the block counts of the round card on a pro-rata pool.
func propRoundLines(ctx *Context, round []string) []string {
	p := ctx.Snap.Prop
	round[3] = kv(ctx, "block reward", 15, propBCH(p.Reward))
	round[4] = kv(ctx, "finder bonus", 15, propBCH(p.FinderBonus))
	round[8] = kv(ctx, "pool work", 15, fmtDiff(ctx.Snap.Round.Total))
	return round
}

// propBCH is a headline amount in BCH, a dash when unknown.
func propBCH(sats int64) string {
	if sats <= 0 {
		return dash
	}
	return fmtEstBCH(sats) + " BCH"
}

// propAddressLines is the estimated reward of an address in the miner view.
func propAddressLines(ctx *Context, addr string) []string {
	t := ctx.Theme
	p := ctx.Snap.Prop
	est, note := dash, ""
	if m, ok := roundMiner(ctx.Snap.Round, addr); ok && p.Reward > 0 {
		est = fmtEstBCH(m.EstSats) + " BCH"
		note = "  estimate until the block confirms"
		if !m.Eligible {
			est = "0 BCH"
			note = "  under 1% of the round: not paid, its work is shared by the others"
		}
	}
	return []string{" " + kv(ctx, "est. reward", 33, est) + t.Dim.Render(note)}
}
