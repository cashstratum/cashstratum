package ui

import (
	"math"
	"strconv"
	"time"
)

// poolShare is the fraction of the network the pool hashes, on the
// difficulty basis: rate over the hashrate that difficulty implies at one
// block per 600 s (D 2^32 / 600). It is the same basis as the expected-blocks
// figure, so share x 144 equals the blocks per day. Zero when unknown.
func poolShare(rate, diff float64) float64 {
	if rate <= 0 || diff <= 0 {
		return 0
	}
	return rate / (diff * 4294967296 / 600)
}

// fmtPoolShare prints a share fraction as a percentage with two decimals
// ("1.70%"). Below 0.01% two decimals would print 0.00%, so it switches to
// parts per million with two significant figures.
func fmtPoolShare(f float64) string {
	if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return dash
	}
	if pct := f * 100; pct >= 0.01 {
		return strconv.FormatFloat(pct, 'f', 2, 64) + "%"
	}
	return fmtSig(f*1e6) + " ppm"
}

func renderNetwork(ctx *Context, w, h int) []string {
	n := ctx.Snap.Network
	p := ctx.Snap.Pool
	t := ctx.Theme
	rate := poolRate(p)
	share := fmtPoolShare(poolShare(rate, n.Difficulty))
	perDay := dash
	if eta, ok := ETA(rate, n.Difficulty); ok && eta > 0 {
		v := float64(day) / float64(eta)
		if v < 1 {
			perDay = fmtSig(v) + " blocks/day"
		} else {
			perDay = fmtSI(v) + " blocks/day"
		}
	}
	rpc := t.Bad.Render("unreachable")
	if n.RPCOK {
		rpc = t.Good.Render("ok") + "  " + fmtLatency(n.RPCLatency)
	}
	chain := n.Chain
	if chain == "" {
		chain = dash
	}
	tipAge := dash
	if !n.TipTime.IsZero() {
		tipAge = fmtCoarse(ctx.Now.Sub(n.TipTime)) + " ago"
	}
	node := []string{
		kv(ctx, "chain", 16, chain),
		kv(ctx, "tip", 16, heightStr(n.Tip)),
		kv(ctx, "tip hash", 16, shortHash(n.TipHash, 12, ctx.sym.Ellipsis)),
		kv(ctx, "tip age", 16, tipAge),
		kv(ctx, "difficulty", 16, fmtDiff(n.Difficulty)),
		kv(ctx, "net hashrate", 16, fmtHash(n.NetHashrate)),
		kv(ctx, "mempool", 16, fmtInt(int64(n.MempoolTx))+" tx, "+fmtSI(float64(n.MempoolBytes))+"B"),
	}
	health := []string{
		kv(ctx, "rpc", 16, rpc),
		kv(ctx, "zmq hashblock", 16, zmqLabel(ctx.Now, n)),
		kv(ctx, "template age", 16, ageCoarse(ctx.Now, n.TemplateTime)),
		kv(ctx, "template txs", 16, fmtInt(int64(n.TemplateTxs))),
		"",
		kv(ctx, "pool share", 16, share),
		kv(ctx, "expected", 16, perDay),
		kv(ctx, "P(block 1h)", 16, fmtPct(ProbBlockWithin(rate, n.Difficulty, time.Hour)*100)),
		kv(ctx, "P(block 24h)", 16, fmtPct(ProbBlockWithin(rate, n.Difficulty, day)*100)),
	}
	half := (w - 1) / 2
	bh := max(len(health), len(node)) + 2
	return hjoin(box(ctx, "Node", node, half, bh), box(ctx, "Health & odds", health, w-1-half, bh))
}
