package poolindex

import (
	"sort"
	"time"

	"cashstratumapi/internal/poolfmt"
)

// OnlineWindow is how recent an address's last accepted share must be for
// the address to count as online.
const OnlineWindow = 10 * time.Minute

// RoundAddress is one canonical address's share of the live round.
type RoundAddress struct {
	Address     string // canonical address (poolfmt.CanonicalAddress)
	Username    string // the username of its newest accepted share, as submitted
	Work        float64
	Shares      int64
	LastShareAt time.Time
	BestShare   float64 // highest sdiff over its workers, block-sized shares excluded
	Online      bool    // LastShareAt within OnlineWindow of the time asked
}

// LiveRound is the pool round since the last accepted block.
type LiveRound struct {
	// Height and StartedAt identify the accepted block the round started
	// after; both are zero before the pool's first block.
	Height      int64
	StartedAt   time.Time
	TotalWork   float64
	NetworkDiff float64 // newest network difficulty seen in the log
	// Seen reports that sharelogs exist for the round at all.
	Seen bool
	// Addresses holds every address with work, most work first.
	Addresses []RoundAddress
}

// LiveRound returns the current round, judging Online against now.
func (ix *Index) LiveRound(now time.Time) LiveRound {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	r := ix.st.Round
	lr := LiveRound{Height: r.Height, StartedAt: r.Since, NetworkDiff: ix.st.NetDiff, Seen: r.Seen}
	best := map[string]float64{}
	for k, b := range r.Best {
		addr, _ := poolfmt.SplitWorker(k)
		addr = poolfmt.CanonicalAddress(addr)
		if b.Diff > best[addr] {
			best[addr] = b.Diff
		}
	}
	for a, w := range r.Sums {
		if w <= 0 {
			continue
		}
		st := r.Addrs[a]
		lr.TotalWork += w
		lr.Addresses = append(lr.Addresses, RoundAddress{
			Address: a, Username: st.Username, Work: w, Shares: st.Shares, LastShareAt: st.Last,
			BestShare: best[a], Online: !st.Last.IsZero() && now.Sub(st.Last) <= OnlineWindow,
		})
	}
	sort.Slice(lr.Addresses, func(i, j int) bool {
		if lr.Addresses[i].Work != lr.Addresses[j].Work {
			return lr.Addresses[i].Work > lr.Addresses[j].Work
		}
		return lr.Addresses[i].Address < lr.Addresses[j].Address
	})
	return lr
}

// HashrateSeries returns the per-minute pool hashrate points (H/s) whose
// minute starts in [from, to), oldest first. A zero to means no upper bound.
func (ix *Index) HashrateSeries(from, to time.Time) []Point {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var out []Point
	for _, p := range ix.st.Hashrate {
		if inRange(p.T, from, to) {
			out = append(out, p)
		}
	}
	return out
}

// BestShareSeries returns the per-minute pool best share (the highest sdiff
// of an accepted share, block-sized shares included) for minutes starting in
// [from, to), oldest first; minutes without accepted shares are absent. A
// zero to means no upper bound. The series reaches back seven days.
func (ix *Index) BestShareSeries(from, to time.Time) []Point {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var out []Point
	for m, v := range ix.st.BestSeries {
		if inRange(m, from, to) {
			out = append(out, Point{T: m, V: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

func inRange(t int64, from, to time.Time) bool {
	if t < from.Unix() {
		return false
	}
	return to.IsZero() || t < to.Unix()
}
