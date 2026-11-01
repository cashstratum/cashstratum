package index

import (
	"sort"
	"strings"
	"time"

	"cashstratumapi/cmd/cashtop/internal/tail"
	"cashstratumapi/internal/poolfmt"
)

// Block statuses.
const (
	StatusAccepted = "accepted"
	StatusRejected = "rejected"
	StatusOrphaned = "orphaned"
)

const (
	bestShareN   = 50
	seriesBucket = 60 // seconds per downsampled point
	seriesMax    = 7 * 24 * 60
)

// Block is one block the pool submitted.
type Block struct {
	Height int64     `json:"height"`
	Hash   string    `json:"hash,omitempty"`
	Finder string    `json:"finder,omitempty"`
	Proto  string    `json:"proto,omitempty"`
	Time   time.Time `json:"time"`
	Luck   *float64  `json:"luck,omitempty"`
	Status string    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	Offset int64     `json:"offset,omitempty"`
	Reward int64     `json:"reward,omitempty"` // satoshis; zero until looked up
}

// Worker tracks the sessions of one full worker name.
type Worker struct {
	Name       string    `json:"name"`
	Address    string    `json:"address"`
	Clients    int       `json:"clients"`
	Online     bool      `json:"online"`
	Since      time.Time `json:"since"`
	LastDrop   time.Time `json:"last_drop"`
	LastReason string    `json:"last_reason,omitempty"`
	Sessions   int       `json:"sessions"`
	Drops      int       `json:"drops"`
	Longest    int64     `json:"longest_s"`
	// Agent and LastDiff come from the newest sharelog line seen for the
	// worker, at any height; snapshots written before they existed lack them.
	Agent    string  `json:"agent,omitempty"`
	LastDiff float64 `json:"last_diff,omitempty"`
}

// Streak is the length of the current uninterrupted session at now.
func (w Worker) Streak(now time.Time) time.Duration {
	if !w.Online || w.Since.IsZero() {
		return 0
	}
	return now.Sub(w.Since)
}

// BestShare is one entry of the top-N best-share table.
type BestShare struct {
	Address string    `json:"address"`
	Diff    float64   `json:"diff"`
	Time    time.Time `json:"time"`
}

// Point is one downsampled series sample.
type Point struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// Work is the per-address accepted work since the current network block,
// summed from the sharelog files of the newest height directory.
type Work struct {
	Height int64              `json:"height"`
	Sums   map[string]float64 `json:"sums"`
	Files  map[string]int64   `json:"files"`
}

// Round is the best accepted share per worker since the pool's last accepted
// block, read from the sharelogs. Since and Height identify that block; a
// different block starts the round over. Files holds the read offsets of
// every sharelog scanned for the round, keyed "dir/file".
type Round struct {
	Since  time.Time            `json:"since"`
	Height int64                `json:"height"`
	Seen   bool                 `json:"seen"`
	Files  map[string]int64     `json:"files"`
	Best   map[string]BestShare `json:"best"`
}

func newRound() Round {
	return Round{Files: map[string]int64{}, Best: map[string]BestShare{}}
}

// State is everything the index remembers.
type State struct {
	Cursor     tail.Cursor        `json:"cursor"`
	Blocks     map[int64]*Block   `json:"blocks"`
	Workers    map[string]*Worker `json:"workers"`
	Best       []BestShare        `json:"best"`
	Hashrate   []Point            `json:"hashrate"`
	PoolLuck   []Point            `json:"pool_luck"`
	Work       Work               `json:"work"`
	Round      Round              `json:"round"`
	LastZMQ    string             `json:"last_zmq,omitempty"`
	LastReject string             `json:"last_reject,omitempty"`
	OpenSolve  int64              `json:"open_solve,omitempty"`
	OpenLuck   *float64           `json:"open_luck,omitempty"`
	NetDiff    float64            `json:"net_diff,omitempty"` // newest network difficulty seen in the log
	Solved     time.Time          `json:"solved,omitzero"`    // log time of the newest accepted block
	Tip        int64              `json:"tip,omitempty"`      // last chain height read from the node
}

func newState() *State {
	return &State{
		Blocks:  map[int64]*Block{},
		Workers: map[string]*Worker{},
		Work:    Work{Sums: map[string]float64{}, Files: map[string]int64{}},
		Round:   newRound(),
	}
}

// dirty collects what changed since the last flush.
type dirty struct {
	blocks   map[int64]bool
	workers  map[string]bool
	best     bool
	hrFrom   int // first changed index in Hashrate, -1 when clean
	luckFrom int
	work     map[string]bool
	files    map[string]bool
	workRst  bool
	round    bool
	trans    bool
}

func newDirty(s *State) *dirty {
	return &dirty{
		blocks: map[int64]bool{}, workers: map[string]bool{},
		hrFrom: -1, luckFrom: -1,
		work: map[string]bool{}, files: map[string]bool{},
	}
}

func (d *dirty) empty() bool {
	return len(d.blocks) == 0 && len(d.workers) == 0 && !d.best && d.hrFrom < 0 &&
		d.luckFrom < 0 && len(d.work) == 0 && len(d.files) == 0 && !d.workRst && !d.round && !d.trans
}

// applyEvent folds one parsed log event into the state.
func (ix *Index) applyEvent(ev poolfmt.Event, off int64) {
	s, d := ix.st, ix.dirty
	switch ev.Kind {
	case poolfmt.KindAuthorised:
		w := ix.worker(ev.Worker, ev.Address, ev.User)
		w.Clients++
		w.Sessions++
		if !w.Online {
			w.Online, w.Since = true, ev.Time
		}
		d.workers[w.Name] = true
	case poolfmt.KindDropped:
		if ev.Worker == "" {
			return
		}
		w := ix.worker(ev.Worker, "", ev.User)
		w.Drops++
		w.LastDrop = ev.Time
		if ev.Reason.OK {
			w.LastReason = ev.Reason.V
		}
		if w.Clients > 0 {
			w.Clients--
		}
		if w.Clients == 0 && w.Online {
			w.Online = false
			if secs := int64(ev.Time.Sub(w.Since) / time.Second); secs > w.Longest {
				w.Longest = secs
			}
		}
		d.workers[w.Name] = true
	case poolfmt.KindPool:
		if !ev.HasTime {
			return
		}
		if ev.Pool.HasHashrate {
			ix.addPoint(&s.Hashrate, &d.hrFrom, ev.Time, ev.Pool.Hashrate1m)
		}
		if ev.Pool.HasShares {
			ix.addPoint(&s.PoolLuck, &d.luckFrom, ev.Time, ev.Pool.Luck)
		}
	case poolfmt.KindNetworkDiff:
		if ev.Diff > 0 && ev.Diff != s.NetDiff {
			s.NetDiff, d.trans = ev.Diff, true
		}
	case poolfmt.KindUser:
		if nd := ev.Stats.NetworkDiff; nd.OK && nd.V > 0 && nd.V != s.NetDiff {
			s.NetDiff, d.trans = nd.V, true
		}
		if b := ev.Stats.BestShare; b.OK && b.V > 0 && ev.HasTime && ev.Time.After(s.Solved) {
			if ev.Stats.NetworkDiff.OK && ev.Stats.NetworkDiff.V > 0 && b.V >= ev.Stats.NetworkDiff.V {
				break
			}
			ix.noteBest(poolfmt.NormalizeAddress(ev.Name), b.V, ev.Time)
		}
	case poolfmt.KindZMQBlock, poolfmt.KindSV2Hash:
		s.LastZMQ, d.trans = ev.Hash, true
	case poolfmt.KindSubmitReturned:
		s.LastReject, d.trans = ev.Message, true
	case poolfmt.KindSolved, poolfmt.KindSV2Accepted:
		b := ix.block(ev.Height)
		luck := b.Luck
		if luck == nil && s.OpenLuck != nil {
			luck = s.OpenLuck
		}
		*b = Block{Height: ev.Height, Hash: s.LastZMQ, Finder: ev.Worker, Proto: ev.Protocol,
			Time: ev.Time, Status: StatusAccepted, Offset: off, Reward: b.Reward, Luck: luck}
		if old, ok := ix.carry[ev.Height]; ok && (old.Time.IsZero() || old.Time.Equal(b.Time)) {
			if old.Reward > 0 {
				b.Reward = old.Reward
			}
			if old.Hash != "" {
				b.Hash = old.Hash
			}
			if b.Luck == nil && old.Luck != nil {
				l := *old.Luck
				b.Luck = &l
			}
			if b.Proto == "" && old.Proto != "" {
				b.Proto = old.Proto
			}
		}
		if prev := ix.orphans[ev.Height]; prev {
			b.Status = StatusOrphaned
		}
		s.OpenSolve, s.OpenLuck, d.trans = ev.Height, nil, true
		d.blocks[ev.Height] = true
		if ev.Time.After(s.Solved) {
			s.Solved = ev.Time
			if len(s.Best) > 0 {
				s.Best, d.best = nil, true
			}
		}
	case poolfmt.KindSolveShares:
		l := ev.Percent
		if b := s.Blocks[s.OpenSolve]; b != nil {
			b.Luck = &l
			d.blocks[b.Height] = true
			s.OpenSolve, s.OpenLuck, d.trans = 0, nil, true
		} else {
			s.OpenLuck, d.trans = &l, true
		}
	case poolfmt.KindBlockRejected:
		b := ix.block(ev.Height)
		if b.Status != StatusAccepted {
			*b = Block{Height: ev.Height, Time: ev.Time, Status: StatusRejected, Reason: s.LastReject, Offset: off}
			d.blocks[ev.Height] = true
		}
	}
}

func (ix *Index) block(h int64) *Block {
	b := ix.st.Blocks[h]
	if b == nil {
		b = &Block{Height: h}
		ix.st.Blocks[h] = b
	}
	return b
}

func (ix *Index) worker(name, client, user string) *Worker {
	w := ix.st.Workers[name]
	if w == nil {
		addr, _ := poolfmt.SplitWorker(name)
		if user != "" {
			addr = user
		}
		w = &Worker{Name: name, Address: poolfmt.NormalizeAddress(addr)}
		ix.st.Workers[name] = w
	}
	return w
}

func (ix *Index) addPoint(series *[]Point, from *int, t time.Time, v float64) {
	bucket := t.Unix() / seriesBucket * seriesBucket
	s := *series
	if n := len(s); n > 0 && s[n-1].T == bucket {
		s[n-1].V = v
		if *from < 0 || *from > n-1 {
			*from = n - 1
		}
		return
	} else if n > 0 && s[n-1].T > bucket {
		return // out of order
	}
	*series = append(s, Point{T: bucket, V: v})
	if *from < 0 || *from > len(s) {
		*from = len(s)
	}
}

func (ix *Index) noteBest(addr string, diff float64, t time.Time) {
	s := ix.st
	for i := range s.Best {
		if s.Best[i].Address == addr {
			if diff <= s.Best[i].Diff {
				return
			}
			s.Best[i].Diff, s.Best[i].Time = diff, t
			ix.sortBest()
			ix.dirty.best = true
			return
		}
	}
	if len(s.Best) >= bestShareN {
		if diff <= s.Best[len(s.Best)-1].Diff {
			return
		}
		s.Best = s.Best[:len(s.Best)-1]
	}
	s.Best = append(s.Best, BestShare{Address: addr, Diff: diff, Time: t})
	ix.sortBest()
	ix.dirty.best = true
}

func (ix *Index) sortBest() {
	b := ix.st.Best
	sort.SliceStable(b, func(i, j int) bool { return b[i].Diff > b[j].Diff })
}

// isLineOfInterest is a cheap prefix filter so the scan only pays for the
// parser on lines the index uses.
func isLineOfInterest(line string) bool {
	body := line
	if len(line) > 0 && line[0] == '[' {
		if i := strings.Index(line, "] "); i >= 0 {
			body = line[i+2:]
		}
	}
	if len(body) < 4 {
		return false
	}
	switch body[0] {
	case 'A': // Authorised
		return strings.HasPrefix(body, "Authorised ")
	case 'D':
		return strings.HasPrefix(body, "Dropped ")
	case 'P':
		return strings.HasPrefix(body, "Pool:")
	case 'U':
		return strings.HasPrefix(body, "User ")
	case 'Z':
		return strings.HasPrefix(body, "ZMQ ")
	case 'S':
		return strings.HasPrefix(body, "Solved ") || strings.HasPrefix(body, "SUBMIT BLOCK RETURNED") ||
			strings.HasPrefix(body, "Submitted, but") || strings.HasPrefix(body, "SV2/JD block hash")
	case 'B':
		return strings.HasPrefix(body, "Block ")
	}
	return false
}
