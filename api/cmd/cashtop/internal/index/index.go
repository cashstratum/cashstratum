// Package index keeps a small on-disk cache of what cashtop has learned from
// a pool install, so a restart resumes from the saved log offset instead of
// re-reading the whole log. The pool's own files are only ever read; all
// writes go to the per-install cache directory.
package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"cashstratumapi/cmd/cashtop/internal/tail"
	"cashstratumapi/internal/poolfmt"
)

// Options configures Open.
type Options struct {
	// Root is the pool install directory (the one holding logs/).
	Root string
	// LogPath overrides <Root>/logs/cashstratum.log.
	LogPath string
	// CacheDir overrides the cache location ($XDG_CACHE_HOME/cashtop/<hash>).
	CacheDir string
	// ForceRescan ignores any saved state.
	ForceRescan bool
	// Memory keeps the index in RAM only and never touches the cache dir.
	Memory bool
	// Tail tunes the log tailer.
	Tail tail.Options
	// WorkEvery throttles sharelog and solved-heights checks in Run.
	WorkEvery time.Duration
}

// Info describes how Open obtained its state.
type Info struct {
	Persistent bool          // state is saved to the cache directory
	Rescanned  bool          // the log was read from the start
	Reason     string        // why a rescan was needed, if it was
	Lines      int64         // log lines examined by the Open scan
	Duration   time.Duration // time spent in Open
	CacheDir   string
}

// Index is the live index. All methods are safe for concurrent use.
type Index struct {
	opt  Options
	mu   sync.Mutex
	dir  string
	lock *os.File

	st      *State
	dirty   *dirty
	written tail.Cursor
	tl      *tail.Tailer
	journal *os.File
	jbytes  int64
	gen     int
	orphans map[int64]bool
	carry   map[int64]Block // enrichment from before a rescan, by height
	ctxRead map[int64]bool  // solves/<height>-<hash>.log already parsed

	sigIdx, sigTSV fileSig
	lastWork       time.Time
	info           Info
	lines          int64
	join           poolfmt.Joiner
}

type fileSig struct {
	ino  uint64
	size int64
	mod  int64
}

// CacheDirFor returns the cache directory used for an install directory.
func CacheDirFor(root string) string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".cache")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return filepath.Join(base, "cashtop", hex.EncodeToString(sum[:8]))
}

// Open loads the saved state, validates it against the log, and brings it up
// to date: an incremental scan from the saved cursor when the log still
// matches, otherwise a full scan. It never fails because of a bad cache.
func Open(opt Options) (*Index, error) {
	start := time.Now()
	if opt.LogPath == "" {
		opt.LogPath = filepath.Join(opt.Root, "logs", "cashstratum.log")
	}
	if opt.WorkEvery <= 0 {
		opt.WorkEvery = 5 * time.Second
	}
	ix := &Index{opt: opt, orphans: map[int64]bool{}, carry: map[int64]Block{}, ctxRead: map[int64]bool{}}
	ix.st = newState()
	ix.dirty = newDirty(ix.st)

	reason := ""
	if !opt.Memory {
		dir := opt.CacheDir
		if dir == "" {
			dir = CacheDirFor(opt.Root)
		}
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if lk, err := tryLock(filepath.Join(dir, "lock")); err == nil && lk != nil {
				ix.dir, ix.lock = dir, lk
				stale, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
				for _, p := range stale {
					os.Remove(p)
				}
			}
		}
	}
	if ix.dir != "" {
		gen, err := ix.load()
		switch {
		case err != nil:
			reason = "no usable cache: " + err.Error()
			ix.resetState()
		case opt.ForceRescan:
			reason = "forced"
			ix.resetState()
		default:
			ix.gen = gen
			ix.written = ix.st.Cursor
		}
	} else if opt.ForceRescan {
		reason = "forced"
	}

	var from *tail.Cursor
	if c := ix.st.Cursor; c.Inode != 0 || c.Offset != 0 {
		from = &c
	}
	tl, err := tail.New(opt.LogPath, from, opt.Tail)
	if err == tail.ErrCursorMismatch {
		reason = "log changed since the cursor was saved"
		ix.resetState()
		from = nil
		tl, err = tail.New(opt.LogPath, nil, opt.Tail)
	}
	if err != nil {
		ix.Close()
		return nil, err
	}
	ix.tl = tl
	if reason == "" && from == nil {
		reason = "no saved cursor"
	}
	if reason == "" && !opt.Memory && ix.dir == "" {
		reason = "cache locked by another cashtop"
	}
	ix.info.Rescanned = from == nil
	ix.info.Reason = reason
	ix.info.Persistent = ix.dir != ""
	ix.info.CacheDir = ix.dir

	ix.mu.Lock()
	err = ix.syncLocked(true)
	ix.mu.Unlock()
	ix.info.Lines = ix.lines
	ix.info.Duration = time.Since(start)
	if err != nil {
		ix.Close()
		return nil, err
	}
	return ix, nil
}

// resetState drops everything but remembers what the log cannot give back:
// which blocks were marked orphaned, the node's hash and reward per height,
// and the chain tip.
func (ix *Index) resetState() {
	for h, b := range ix.st.Blocks {
		if b.Status == StatusOrphaned {
			ix.orphans[h] = true
		}
		if b.Reward > 0 || b.Hash != "" || b.Luck != nil || b.Proto != "" {
			ix.carry[h] = *b
		}
	}
	tip := ix.st.Tip
	ix.st = newState()
	ix.st.Tip = tip
	ix.dirty = newDirty(ix.st)
	ix.dirty.trans = tip > 0
	ix.written = tail.Cursor{}
	ix.join = poolfmt.Joiner{}
	ix.gen = 0
	if ix.journal != nil {
		ix.journal.Close()
		ix.journal = nil
	}
	if ix.dir != "" {
		os.Remove(filepath.Join(ix.dir, snapshotName))
		os.Remove(filepath.Join(ix.dir, journalName))
	}
}

// Info returns how Open obtained the state.
func (ix *Index) Info() Info {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.info
}

// Close flushes and releases the cache lock.
func (ix *Index) Close() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var err error
	if ix.dir != "" && ix.st != nil {
		err = ix.flush()
	}
	if ix.tl != nil {
		ix.tl.Close()
		ix.tl = nil
	}
	if ix.journal != nil {
		ix.journal.Close()
		ix.journal = nil
	}
	if ix.lock != nil {
		ix.lock.Close()
		ix.lock = nil
	}
	return err
}

// Sync reads new log lines, sharelog work and solved heights, then saves.
func (ix *Index) Sync() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.syncLocked(true)
}

// Run keeps the index current until ctx is done.
func (ix *Index) Run(ctx context.Context) {
	poll := ix.opt.Tail.Poll
	if poll <= 0 {
		poll = tail.DefaultPoll
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			ix.mu.Lock()
			full := time.Since(ix.lastWork) >= ix.opt.WorkEvery
			_ = ix.syncLocked(full)
			ix.mu.Unlock()
		}
	}
}

func (ix *Index) syncLocked(full bool) error {
	if ix.tl == nil {
		return nil
	}
	fed := 0
	pollErr := ix.tl.Poll(func(l tail.Line) { fed++; ix.applyLineLocked(l) })
	if fed == 0 {
		// Nothing new arrived since the last poll, so a held line is complete.
		for _, j := range ix.join.Flush() {
			ix.applyJoined(j)
		}
	}
	if c := ix.tl.Cursor(); c.Inode != 0 {
		// A held first chunk has not been applied yet; a restart must
		// read it again.
		if off, ok := ix.join.Pending(); ok && off < c.Offset {
			c.Offset = off
		}
		ix.st.Cursor = c
	}
	if full {
		ix.lastWork = time.Now()
		ix.syncSolved()
		ix.syncWork()
		ix.syncRound()
	}
	if err := ix.flush(); err != nil {
		return err
	}
	return pollErr
}

func (ix *Index) applyLineLocked(l tail.Line) {
	ix.lines++
	for _, j := range ix.join.Feed(l.Text, l.Offset) {
		ix.applyJoined(j)
	}
}

func (ix *Index) applyJoined(j poolfmt.Joined) {
	if !isLineOfInterest(j.Line) {
		return
	}
	ev := poolfmt.ParseLine(j.Line)
	if ev.Kind == poolfmt.KindUnknown {
		return
	}
	ix.applyEvent(ev, j.Offset)
}

// Apply folds one event into the state. Callers that tail the log
// themselves pass the line's byte offset so blocks keep their log order.
func (ix *Index) Apply(ev poolfmt.Event, offset int64) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.applyEvent(ev, offset)
}

// SetBlockReward stores the coinbase reward, in satoshis, of a known height.
func (ix *Index) SetBlockReward(height, sats int64) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if b := ix.st.Blocks[height]; b != nil && sats > 0 && b.Reward != sats {
		b.Reward = sats
		ix.dirty.blocks[height] = true
		_ = ix.flush()
	}
}

// SetBlockHash stores the node's hash for a known height.
func (ix *Index) SetBlockHash(height int64, hash string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if b := ix.st.Blocks[height]; b != nil && hash != "" && b.Hash != hash {
		b.Hash = hash
		ix.dirty.blocks[height] = true
		_ = ix.flush()
	}
}

// SetTip remembers the chain height last read from the node, so block
// confirmations show before the node answers again.
func (ix *Index) SetTip(tip int64) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if tip > 0 && ix.st.Tip != tip {
		ix.st.Tip, ix.dirty.trans = tip, true
		_ = ix.flush()
	}
}

// SetBlockStatus records a verdict such as orphaned for a known height.
func (ix *Index) SetBlockStatus(height int64, status string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if b := ix.st.Blocks[height]; b != nil && b.Status != status {
		b.Status = status
		ix.dirty.blocks[height] = true
		if status == StatusOrphaned {
			ix.orphans[height] = true
		}
	}
}

// View is a copy of the index state that is safe to read without locks.
type View struct {
	Cursor   tail.Cursor
	Blocks   []Block // newest height first
	Workers  []Worker
	Best     []BestShare
	Hashrate []Point
	PoolLuck []Point
	Work     Work

	// RoundBest is the best accepted share per worker since the last
	// accepted block, highest first; RoundSeen reports that sharelogs exist.
	RoundBest []BestShare
	RoundSeen bool
	Tip       int64
}

// Snapshot copies the current state.
func (ix *Index) Snapshot() View {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	s := ix.st
	v := View{Cursor: s.Cursor, Best: append([]BestShare(nil), s.Best...),
		Hashrate: append([]Point(nil), s.Hashrate...), PoolLuck: append([]Point(nil), s.PoolLuck...),
		Work: Work{Height: s.Work.Height, Sums: map[string]float64{}, Files: map[string]int64{}}}
	v.RoundSeen, v.Tip = s.Round.Seen, s.Tip
	for _, b := range s.Round.Best {
		v.RoundBest = append(v.RoundBest, b)
	}
	sort.Slice(v.RoundBest, func(i, j int) bool {
		if v.RoundBest[i].Diff != v.RoundBest[j].Diff {
			return v.RoundBest[i].Diff > v.RoundBest[j].Diff
		}
		return v.RoundBest[i].Address < v.RoundBest[j].Address
	})
	if len(v.RoundBest) > bestShareN {
		v.RoundBest = v.RoundBest[:bestShareN]
	}
	for k, x := range s.Work.Sums {
		v.Work.Sums[k] = x
	}
	for k, x := range s.Work.Files {
		v.Work.Files[k] = x
	}
	for _, b := range s.Blocks {
		v.Blocks = append(v.Blocks, *b)
	}
	sort.Slice(v.Blocks, func(i, j int) bool { return v.Blocks[i].Height > v.Blocks[j].Height })
	for _, w := range s.Workers {
		v.Workers = append(v.Workers, *w)
	}
	sort.Slice(v.Workers, func(i, j int) bool { return v.Workers[i].Name < v.Workers[j].Name })
	return v
}
