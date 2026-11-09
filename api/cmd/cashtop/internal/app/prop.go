package app

import (
	"context"
	"encoding/json"
	"os"

	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/poolindex"
)

// propFeePct is the pool fee that marks a pro-rata pool: half of every
// coinbase goes to the payroll output.
const propFeePct = poolindex.PayrollPct

// IsProp reports whether the pool configuration describes a pro-rata pool: a
// 50% poolfee with a pooladdress set. Solo mode is the daemon's -B flag, not a
// conf key, so it is not part of the test.
func IsProp(conf string) bool {
	data, err := os.ReadFile(conf)
	if err != nil {
		return false
	}
	var cf struct {
		PoolFee     float64 `json:"poolfee"`
		PoolAddress string  `json:"pooladdress"`
	}
	if json.Unmarshal(data, &cf) != nil {
		return false
	}
	return cf.PoolFee == propFeePct && cf.PoolAddress != ""
}

// refreshCoinbase reads the current template's coinbase value. A failure
// keeps the last value.
func (a *App) refreshCoinbase(ctx context.Context) {
	if a.node == nil || !a.prop {
		return
	}
	c, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	v, err := a.node.TemplateCoinbaseValue(c)
	if err != nil {
		a.cfg.Log.Printf("template coinbase value: %v", err)
		return
	}
	a.mu.Lock()
	a.coinbase = v
	a.mu.Unlock()
}

// applyProp fills the pro-rata estimates of the round from the coinbase
// value. Without a known value the snapshot only marks the pool as pro rata.
func applyProp(s *ui.Snapshot, coinbase int64) {
	s.Prop = ui.PropInfo{On: true}
	if coinbase <= 0 {
		return
	}
	work := make(map[string]float64, len(s.Round.Miners))
	for _, m := range s.Round.Miners {
		work[m.Address] = m.Diff
	}
	pot, est := poolindex.EstimatePayouts(coinbase, work)
	byAddr := make(map[string]poolindex.PayoutEstimate, len(est))
	for _, e := range est {
		byAddr[e.Address] = e
	}
	s.Prop.Reward, s.Prop.MinersPot = coinbase, pot
	s.Prop.FinderBonus = coinbase - poolindex.PayrollOutput(coinbase)
	for i := range s.Round.Miners {
		e := byAddr[s.Round.Miners[i].Address]
		s.Round.Miners[i].EstSats, s.Round.Miners[i].Eligible = e.Sats, e.Eligible
		s.Prop.MinersShown++
		if !e.Eligible {
			s.Prop.BelowMinimum++
		}
	}
}
