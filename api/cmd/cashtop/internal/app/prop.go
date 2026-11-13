package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

// MinersPotBP reads the miners' pot, in basis points of the coinbase, from
// the pool configuration's "miners_pot_bp" key. The daemon ignores the key;
// it only tells cashtop how the operator splits the payroll output. An
// absent key, or a conf that cannot be read, gives DefaultMinersPotBP with
// no error. A value that is not a whole number in (0, PayrollBP] gives
// DefaultMinersPotBP and an error, so the caller can say so.
func MinersPotBP(conf string) (int64, error) {
	data, err := os.ReadFile(conf)
	if err != nil {
		return poolindex.DefaultMinersPotBP, nil
	}
	var cf struct {
		MinersPotBP *float64 `json:"miners_pot_bp"`
	}
	if json.Unmarshal(data, &cf) != nil || cf.MinersPotBP == nil {
		return poolindex.DefaultMinersPotBP, nil
	}
	v := *cf.MinersPotBP
	if v != math.Trunc(v) || !poolindex.ValidMinersPotBP(int64(v)) {
		return poolindex.DefaultMinersPotBP, fmt.Errorf("miners_pot_bp %v must be a whole number from 1 to %d; using %d",
			v, poolindex.PayrollBP, poolindex.DefaultMinersPotBP)
	}
	return int64(v), nil
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
// value and the miners' pot in basis points. Without a known coinbase value
// the snapshot only marks the pool as pro rata.
func applyProp(s *ui.Snapshot, coinbase, minersBP int64) {
	s.Prop = ui.PropInfo{On: true, MinersPotBP: minersBP}
	if coinbase <= 0 {
		return
	}
	work := make(map[string]float64, len(s.Round.Miners))
	for _, m := range s.Round.Miners {
		work[m.Address] = m.Diff
	}
	pot, est := poolindex.EstimatePayouts(coinbase, minersBP, work)
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
