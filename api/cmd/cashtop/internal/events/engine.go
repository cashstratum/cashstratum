// Package events turns parsed pool log events into block-level outputs:
// submit pre-states, confirmed finds, rejected submits and network blocks.
// Only a "Solved and confirmed block" line past the startup offset and not
// already seen ever produces a BlockFound.
package events

import (
	"time"

	"cashstratumapi/internal/poolfmt"
)

const (
	hashWindow = 1500 * time.Millisecond // ZMQ hash to solve line
	fillWindow = 3 * time.Second         // late hash / luck lines after a solve
	preWindow  = 3 * time.Second
	recentZMQ  = 8 // fallback when lines carry no timestamp: events back
)

// SeenSource lists block heights already known to the host.
type SeenSource interface{ SeenHeights() []int64 }

// Output is one engine result: PreSolve, BlockFound, BlockUpdate,
// BlockRejected or NetworkBlock.
type Output interface{ isOutput() }

// PreSolve is the "submitting" state before a find is confirmed. It is never
// a celebration trigger.
type PreSolve struct {
	Accepted bool // true for BLOCK ACCEPTED!, false for Possible block solve
	Time     time.Time
	Expires  time.Time
}

// BlockFound is a confirmed block solved by the pool.
type BlockFound struct {
	Height    int64
	Finder    string // full address.worker
	Address   string
	Rig       string
	Proto     string
	Hash      string // may be empty until a BlockUpdate arrives
	LuckPct   float64
	Shares    int64
	Time      time.Time
	Reward    float64 // BCH, valid when HasReward
	HasReward bool
	Replayed  bool
}

// BlockUpdate carries fields that arrive after the solve line.
type BlockUpdate struct {
	Height  int64
	Hash    string
	LuckPct float64
	Shares  int64
}

// BlockRejected is a submit the node refused.
type BlockRejected struct {
	Height int64
	Reason string
	Time   time.Time
}

// NetworkBlock is a new chain tip seen through "Block hash changed".
type NetworkBlock struct {
	Hash string
	Ours bool
	Time time.Time
}

func (PreSolve) isOutput()      {}
func (BlockFound) isOutput()    {}
func (BlockUpdate) isOutput()   {}
func (BlockRejected) isOutput() {}
func (NetworkBlock) isOutput()  {}

// SplitFinder splits a finder at the first '.': addresses never contain dots,
// rig names may.
func SplitFinder(full string) (address, rig string) { return poolfmt.SplitWorker(full) }

type zmqSeen struct {
	hash string
	t    time.Time
	has  bool
	n    int // event counter when seen
}

// Engine consumes events in byte-offset order.
type Engine struct {
	startOffset int64
	seen        map[int64]bool
	n           int
	zmq         zmqSeen
	reason      string
	pending     *BlockFound
	pendingN    int
}

// New returns an Engine. Events at or before startOffset never produce
// outputs; solves among them still mark their heights as seen.
func New(startOffset int64, sources ...SeenSource) *Engine {
	e := &Engine{startOffset: startOffset, seen: map[int64]bool{}}
	for _, s := range sources {
		if s != nil {
			e.Seed(s.SeenHeights()...)
		}
	}
	return e
}

// Seed marks heights as already seen.
func (e *Engine) Seed(heights ...int64) {
	for _, h := range heights {
		e.seen[h] = true
	}
}

// Seen reports whether a height was already celebrated or seeded.
func (e *Engine) Seen(h int64) bool { return e.seen[h] }

// ScanLines marks the heights of every solve line in lines as seen. It is the
// backward scan of the live log done before tailing starts.
func (e *Engine) ScanLines(lines []string) {
	for _, l := range lines {
		if ev := poolfmt.ParseLine(l); ev.Kind == poolfmt.KindSolved {
			e.seen[ev.Height] = true
		}
	}
}

// Replay builds a BlockFound that bypasses dedupe and the startup gate, for an
// explicit replay request.
func Replay(b BlockFound) BlockFound {
	b.Replayed = true
	return b
}

func (e *Engine) within(t time.Time, has bool, ref time.Time, refHas bool, d time.Duration, n, refN int) bool {
	if has && refHas {
		dt := t.Sub(ref)
		return dt >= 0 && dt <= d
	}
	return n-refN <= recentZMQ
}

// Feed processes one event at its byte offset.
func (e *Engine) Feed(ev poolfmt.Event, off int64) []Output {
	e.n++
	live := off > e.startOffset
	switch ev.Kind {
	case poolfmt.KindZMQBlock, poolfmt.KindSV2Hash:
		e.zmq = zmqSeen{hash: ev.Hash, t: ev.Time, has: ev.HasTime, n: e.n}
	case poolfmt.KindPossibleSolve, poolfmt.KindBlockAccepted:
		if live {
			return []Output{PreSolve{Accepted: ev.Kind == poolfmt.KindBlockAccepted, Time: ev.Time, Expires: ev.Time.Add(preWindow)}}
		}
	case poolfmt.KindSubmitReturned:
		e.reason = ev.Message
	case poolfmt.KindBlockRejected:
		r := e.reason
		e.reason = ""
		if live {
			return []Output{BlockRejected{Height: ev.Height, Reason: r, Time: ev.Time}}
		}
	case poolfmt.KindSolved, poolfmt.KindSV2Accepted:
		return e.solved(ev, live)
	case poolfmt.KindSolveShares:
		if p := e.pending; p != nil && e.within(ev.Time, ev.HasTime, p.Time, true, fillWindow, e.n, e.pendingN) {
			p.LuckPct, p.Shares = ev.Percent, ev.Count
			return []Output{BlockUpdate{Height: p.Height, Hash: p.Hash, LuckPct: ev.Percent, Shares: ev.Count}}
		}
	case poolfmt.KindBlockHashChanged:
		var out []Output
		if p := e.pending; p != nil && p.Hash == "" && e.within(ev.Time, ev.HasTime, p.Time, true, fillWindow, e.n, e.pendingN) {
			p.Hash = ev.Hash
			out = append(out, BlockUpdate{Height: p.Height, Hash: p.Hash, LuckPct: p.LuckPct, Shares: p.Shares})
		}
		if live {
			ours := e.pending != nil && e.pending.Hash == ev.Hash
			out = append(out, NetworkBlock{Hash: ev.Hash, Ours: ours, Time: ev.Time})
		}
		return out
	}
	return nil
}

func (e *Engine) solved(ev poolfmt.Event, live bool) []Output {
	dup := e.seen[ev.Height]
	e.seen[ev.Height] = true
	e.pending = nil
	if !live || dup {
		return nil
	}
	addr, rig := SplitFinder(ev.Worker)
	b := BlockFound{Height: ev.Height, Finder: ev.Worker, Address: addr, Rig: rig, Proto: ev.Protocol, Time: ev.Time}
	if e.zmq.hash != "" && e.within(ev.Time, ev.HasTime, e.zmq.t, e.zmq.has, hashWindow, e.n, e.zmq.n) {
		b.Hash = e.zmq.hash
	}
	e.pending, e.pendingN = &b, e.n
	return []Output{b}
}
