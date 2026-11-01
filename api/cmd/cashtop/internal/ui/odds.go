package ui

import (
	"math"
	"time"
)

// two32 is the expected number of hashes per unit of share difficulty.
const two32 = 4294967296.0

// StaleK is how many expected share intervals a worker may stay silent
// before it is shown as stale.
const StaleK = 6

// staleFloor keeps fast workers from flapping to stale between shares.
const staleFloor = time.Minute

// PoolLuck is the round effort: accepted share difficulty as a percentage of
// the network difficulty.
func PoolLuck(accepted, networkDiff float64) float64 {
	if networkDiff <= 0 || accepted < 0 {
		return 0
	}
	return accepted / networkDiff * 100
}

// UserLuck is a user's round shares as a percentage of the network difficulty.
func UserLuck(roundShares, networkDiff float64) float64 {
	if networkDiff <= 0 || roundShares < 0 {
		return 0
	}
	return roundShares * 100 / networkDiff
}

// ProbBlockWithin is the Poisson probability of at least one block within t
// at hashrate h (H/s) and difficulty d: 1 - exp(-h*t / (d*2^32)).
func ProbBlockWithin(h, d float64, t time.Duration) float64 {
	if h <= 0 || d <= 0 || t <= 0 {
		return 0
	}
	return -math.Expm1(-h * t.Seconds() / (d * two32))
}

// ETA is the expected time to a block, d*2^32/h. ok is false when the
// hashrate or difficulty is unknown.
func ETA(h, d float64) (time.Duration, bool) {
	if h <= 0 || d <= 0 {
		return 0, false
	}
	s := d * two32 / h
	if s > float64(math.MaxInt64)/float64(time.Second) {
		return time.Duration(math.MaxInt64), true
	}
	return time.Duration(s * float64(time.Second)), true
}

// ShareInterval is the expected time between shares at difficulty diff.
func ShareInterval(diff, h float64) (time.Duration, bool) { return ETA(h, diff) }

// WorkerState classifies a worker as live, stale, idle or off.
type WorkerState int

const (
	StateLive WorkerState = iota
	StateStale
	StateIdle
	StateOff
)

func (s WorkerState) String() string {
	switch s {
	case StateLive:
		return "live"
	case StateStale:
		return "stale"
	case StateIdle:
		return "idle"
	}
	return "off"
}

// Classify reports a worker's state. With no recent hashrate it is idle when
// still connected and off otherwise; with hashrate it is stale when its last
// share is older than StaleK expected intervals (at least a minute).
func Classify(w WorkerRow, now time.Time) WorkerState {
	if w.Hashrate1m <= 0 && w.Hashrate5m <= 0 {
		if w.Connected.IsZero() {
			return StateOff
		}
		return StateIdle
	}
	limit := staleFloor.Seconds()
	if h := w.Hashrate1m; h > 0 && w.Diff > 0 {
		limit = math.Max(limit, StaleK*w.Diff*two32/h)
	}
	if w.LastShare.IsZero() || now.Sub(w.LastShare).Seconds() > limit {
		return StateStale
	}
	return StateLive
}
