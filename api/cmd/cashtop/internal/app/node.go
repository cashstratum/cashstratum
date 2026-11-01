package app

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"cashstratumapi/cmd/cashtop/internal/events"
	"cashstratumapi/cmd/cashtop/internal/index"
	"cashstratumapi/cmd/cashtop/internal/rpc"
	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/ckconf"
	"cashstratumapi/internal/zmqsub"
)

const rpcTimeout = 3 * time.Second

// loadNode reads the RPC endpoint and ZMQ endpoint from the pool config.
// Any failure only disables that source.
func loadNode(conf string, lg *log.Logger) (*rpc.Client, string) {
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(lg.Writer())
	ep := ckconf.LoadBtcdRPC(conf)
	log.SetOutput(prev)
	log.SetFlags(flags)
	var c *rpc.Client
	if ep != nil {
		c = rpc.New(ep)
	}
	zep := ""
	if data, err := os.ReadFile(conf); err == nil {
		var cf ckconf.Conf
		if json.Unmarshal(data, &cf) == nil {
			zep, _ = ckconf.ResolveZMQEndpoint(cf)
		}
	}
	return c, zep
}

// RefreshNode queries the node once. Failures mark it down and keep the
// last values.
func (a *App) RefreshNode(ctx context.Context) {
	if a.node == nil {
		return
	}
	c, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	lat, err := a.node.Latency(c)
	if err != nil {
		a.mu.Lock()
		a.net.RPCOK = false
		a.net.ZMQConfigured, a.net.ZMQConnected = a.zmqState()
		a.mu.Unlock()
		a.cfg.Log.Printf("node rpc: %v", err)
		return
	}
	n := ui.NetworkInfo{RPCOK: true, RPCLatency: lat}
	if ci, err := a.node.ChainInfo(c); err == nil {
		n.Chain, n.Tip, n.TipHash, n.Difficulty = ci.Chain, int(ci.Blocks), ci.BestBlockHash, ci.Difficulty
		if h, err := a.node.BlockHeader(c, ci.BestBlockHash); err == nil && h.Time > 0 {
			n.TipTime = time.Unix(h.Time, 0).UTC()
		}
	}
	if mi, err := a.node.MiningInfo(c); err == nil {
		n.NetHashrate = mi.NetworkHashPS
	}
	if mp, err := a.node.MempoolInfo(c); err == nil {
		n.MempoolTx, n.MempoolBytes = int(mp.Size), mp.Bytes
	}
	if n.Tip > 0 {
		a.ix.SetTip(int64(n.Tip))
	}
	a.mu.Lock()
	n.ZMQLast, n.TemplateTime, n.TemplateTxs = a.net.ZMQLast, a.net.TemplateTime, a.net.TemplateTxs
	n.ZMQConfigured, n.ZMQConnected = a.zmqState()
	a.net = n
	a.mu.Unlock()
}

func (a *App) nodeLoop(ctx context.Context, send func(tea.Msg)) {
	t := time.NewTicker(a.cfg.RPCEvery)
	defer t.Stop()
	checked := int64(0)
	for {
		a.RefreshNode(ctx)
		if m, ok := a.refreshTip(ctx); ok {
			send(m)
		}
		a.enrichMissing(ctx, enrichPerCycle)
		if n := a.netBlks.Load(); n-checked >= int64(a.cfg.OrphanEvery) {
			checked = n
			a.checkOrphans(ctx, 10)
		}
		send(ui.SnapshotMsg{Snapshot: a.snapshot(false)})
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.kick:
		}
	}
}

// zmqState reports whether a ZMQ endpoint is configured and whether its
// subscriber is connected right now.
func (a *App) zmqState() (configured, connected bool) {
	s := a.zmqSub.Load()
	return a.zmqEP != "", s != nil && s.Connected()
}

func (a *App) zmqLoop(ctx context.Context) {
	sub := zmqsub.New(a.zmqEP, func(string) {
		a.mu.Lock()
		a.net.ZMQLast = a.cfg.Now()
		a.mu.Unlock()
		a.poke()
	})
	a.zmqSub.Store(sub)
	sub.Logf = func(format string, args ...any) {
		if ctx.Err() != nil {
			return // a clean shutdown is not worth a log line
		}
		a.cfg.Log.Printf(format, args...)
	}
	sub.Run(ctx)
}

// Enrichment pacing: attempts per cycle, the RPC time a cycle may spend, and
// the wait before retrying a height whose lookup failed.
const (
	enrichPerCycle = 8
	enrichBudget   = 3 * time.Second
)

var enrichBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, time.Hour}

// retryState tracks failed reward lookups for one height.
type retryState struct {
	fails int
	next  time.Time
}

// enrich fills the reward of one block from the node.
func (a *App) enrich(height int64) error {
	if a.enricher == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*rpcTimeout)
	defer cancel()
	b, err := events.Enrich(ctx, a.enricher, events.BlockFound{Height: height})
	if err != nil {
		return err
	}
	if !b.HasReward {
		return nil
	}
	sats := int64(b.Reward*1e8 + 0.5)
	a.ix.SetBlockReward(height, sats)
	a.ix.SetBlockHash(height, b.Hash)
	a.mu.Lock()
	a.rewards[height] = sats
	delete(a.retries, height)
	a.mu.Unlock()
	a.poke()
	return nil
}

// enrichMissing looks up rewards for the newest accepted blocks that lack
// one. n bounds the attempts; a failed height backs off so it cannot starve
// older blocks, and the walk stops once enrichBudget of time is spent.
func (a *App) enrichMissing(ctx context.Context, n int) {
	start := time.Now()
	for _, b := range a.ix.Snapshot().Blocks {
		if n == 0 || ctx.Err() != nil || time.Since(start) >= enrichBudget {
			return
		}
		a.mu.Lock()
		_, have := a.rewards[b.Height]
		rs := a.retries[b.Height]
		a.mu.Unlock()
		if have || b.Reward > 0 || b.Status != index.StatusAccepted || a.cfg.Now().Before(rs.next) {
			continue
		}
		n--
		if err := a.enrich(b.Height); err != nil {
			rs.fails++
			wait := enrichBackoff[min(rs.fails, len(enrichBackoff))-1]
			rs.next = a.cfg.Now().Add(wait)
			a.mu.Lock()
			a.retries[b.Height] = rs
			a.mu.Unlock()
			a.cfg.Log.Printf("block %d reward lookup failed (retry in %s): %v", b.Height, wait, err)
		}
	}
}

// checkOrphans re-checks the newest accepted blocks against the chain.
func (a *App) checkOrphans(ctx context.Context, n int) {
	for _, b := range a.ix.Snapshot().Blocks {
		if n == 0 || ctx.Err() != nil {
			return
		}
		if b.Status != index.StatusAccepted || b.Hash == "" {
			continue
		}
		n--
		c, cancel := context.WithTimeout(ctx, rpcTimeout)
		orphan, err := events.CheckOrphan(c, a.node, events.BlockFound{Height: b.Height, Hash: b.Hash})
		cancel()
		if err == nil && orphan {
			a.ix.SetBlockStatus(b.Height, index.StatusOrphaned)
		}
	}
}

// RewardsComplete reports whether every accepted block has a reward.
func (a *App) RewardsComplete() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, b := range a.ix.Snapshot().Blocks {
		if b.Status == index.StatusAccepted && b.Reward == 0 && a.rewards[b.Height] == 0 {
			return false
		}
	}
	return true
}
