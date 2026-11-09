package poolindex

import (
	"math/big"
	"sort"
)

// Payout split of a proportional (PROP) pool: the coinbase pays PayrollPct
// percent to the payroll output (the daemon's pool fee output) and the rest
// to the finder; of the payroll, MinerPoints out of PayrollPoints go to the
// miners pro rata and the remainder is the pool fee.
const (
	PayrollPct    = 50
	PayrollPoints = 50
	MinerPoints   = 48
	// DustSats is the daemon's dust threshold: a payroll output below it is
	// not emitted and the finder receives the whole coinbase.
	DustSats = 546
	// EligiblePct is the minimum share of the round's work, in percent, an
	// address needs to be paid.
	EligiblePct = 1
)

// PayoutEstimate is one address's estimated share of the miners' pot.
type PayoutEstimate struct {
	Address  string
	Work     float64
	Eligible bool
	Sats     int64
}

// PayrollOutput is the value of a coinbase's payroll output, computed the
// way the daemon builds the coinbase: (double)coinbase/100*PayrollPct,
// truncated toward zero. The finder output is the coinbase minus this, so an
// odd satoshi goes to the finder. Below DustSats the daemon emits no payroll
// output, and PayrollOutput is zero.
func PayrollOutput(coinbaseSats int64) int64 {
	if coinbaseSats <= 0 {
		return 0
	}
	payroll := int64(float64(coinbaseSats) / 100 * float64(PayrollPct))
	if payroll < DustSats {
		return 0
	}
	return payroll
}

// MinersPot is the part of a coinbase paid to miners pro rata:
// floor(PayrollOutput * MinerPoints / PayrollPoints). It never exceeds the
// payroll output.
func MinersPot(coinbaseSats int64) int64 {
	payroll := big.NewInt(PayrollOutput(coinbaseSats))
	pot := new(big.Int).Quo(new(big.Int).Mul(payroll, big.NewInt(MinerPoints)), big.NewInt(PayrollPoints))
	return pot.Int64()
}

// EstimatePayouts splits the miners' pot of a coinbase across the round's
// addresses by work. It is a pure function.
//
//   - An address is eligible when its work is at least EligiblePct percent
//     of the round's total work. The pot is shared among eligible addresses
//     in proportion to their work, which redistributes the work of
//     ineligible addresses pro rata.
//   - When no address reaches the threshold, every address with work is
//     eligible.
//   - Addresses with zero or negative work are never eligible.
//   - Each payout is floor(pot * work / eligibleWork), computed exactly from
//     the float64 work values, so the payouts never sum to more than the
//     pot. The rounding remainder, at most one satoshi per eligible address,
//     stays unallocated.
//
// It returns the pot and one estimate per address, most work first.
func EstimatePayouts(coinbaseSats int64, work map[string]float64) (pot int64, out []PayoutEstimate) {
	pot = MinersPot(coinbaseSats)
	total := new(big.Rat)
	for a, w := range work {
		out = append(out, PayoutEstimate{Address: a, Work: w})
		if w > 0 {
			total.Add(total, ratOf(w))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Work != out[j].Work {
			return out[i].Work > out[j].Work
		}
		return out[i].Address < out[j].Address
	})
	if total.Sign() == 0 {
		return pot, out
	}
	// work*100 >= total*EligiblePct, kept exact.
	threshold := new(big.Rat).Mul(total, big.NewRat(EligiblePct, 100))
	elig := new(big.Rat)
	found := false
	for i := range out {
		if out[i].Work > 0 && ratOf(out[i].Work).Cmp(threshold) >= 0 {
			out[i].Eligible = true
			elig.Add(elig, ratOf(out[i].Work))
			found = true
		}
	}
	if !found {
		for i := range out {
			if out[i].Work > 0 {
				out[i].Eligible = true
			}
		}
		elig = total
	}
	potR := new(big.Rat).SetInt64(pot)
	for i := range out {
		if !out[i].Eligible {
			continue
		}
		share := new(big.Rat).Mul(potR, ratOf(out[i].Work))
		share.Quo(share, elig)
		out[i].Sats = new(big.Int).Quo(share.Num(), share.Denom()).Int64()
	}
	return pot, out
}

// ratOf is f as an exact rational; a non-finite value counts as zero.
func ratOf(f float64) *big.Rat {
	if r := new(big.Rat).SetFloat64(f); r != nil {
		return r
	}
	return new(big.Rat)
}
