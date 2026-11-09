package poolindex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"cashstratumapi/internal/poolfmt"
)

// The round store keeps one immutable record per solve, outside the index
// cache so that a rescan never touches it:
//
//	<RoundStore>/baseline                          solves at or below this height are never frozen
//	<RoundStore>/<height>/round.json               the frozen record
//	<RoundStore>/<height>/sharelogs/<%08x>/*.sharelog  the round's sharelogs, copied at freeze time
//
// A record is written once, with a no-overwrite link, after its sharelogs
// are archived; its existence marks the freeze complete. Afterwards only its
// status may change, and an empty hash may be filled in once.
const (
	roundFile    = "round.json"
	archiveDir   = "sharelogs"
	baselineFile = "baseline"
)

// FrozenAddress is one canonical address's share of a frozen round.
type FrozenAddress struct {
	Address     string    `json:"address"`
	Username    string    `json:"username"`
	Work        float64   `json:"work"`
	Shares      int64     `json:"shares"`
	LastShareAt time.Time `json:"last_share_at"`
}

// FrozenRound is the round that closed at one solve, as it stood when the
// solve was seen. Work is the sum of the assigned difficulty of accepted
// shares, stored at full float64 precision.
type FrozenRound struct {
	Height          int64           `json:"height"`
	Hash            string          `json:"hash"`
	Status          string          `json:"status"`
	SolvedAt        time.Time       `json:"solved_at"`
	FinderWorker    string          `json:"finder_worker"`
	FinderAddress   string          `json:"finder_address"`
	PrevBlockHeight int64           `json:"prev_block_height"`
	StartedAt       time.Time       `json:"started_at,omitzero"`
	TotalWork       float64         `json:"total_work"`
	Addresses       []FrozenAddress `json:"addresses"`
	FrozenAt        time.Time       `json:"frozen_at"`

	// HashVerified is set once the node has confirmed Hash; until then it is
	// provisional and VerifySolveHash may correct it once.
	HashVerified bool `json:"hash_verified,omitempty"`

	// Node-sourced fields, filled once by SetFrozenNode after the node
	// answers; a nil CoinbaseSats means they are not filled yet.
	CoinbaseSats  *int64       `json:"coinbase_value_sats,omitempty"`
	FinderOutput  *BlockOutput `json:"finder_output,omitempty"`
	PayrollOutput *BlockOutput `json:"payroll_output,omitempty"`
}

// BlockOutput is one coinbase output of a solved block, as the node reports it.
type BlockOutput struct {
	Address string `json:"address"`
	Vout    int    `json:"vout"`
	Sats    int64  `json:"value_sats"`
}

// Solve is the summary of one frozen round.
type Solve struct {
	Height   int64
	Hash     string
	SolvedAt time.Time
	Status   string
}

// ErrUnknownSolve is returned for a height that is neither a known block nor
// a frozen round.
var ErrUnknownSolve = errors.New("poolindex: unknown solve height")

func (fr FrozenRound) clone() FrozenRound {
	fr.Addresses = append([]FrozenAddress(nil), fr.Addresses...)
	return fr
}

// openRoundStore creates the store directory and loads every record in it.
// Records that cannot be read still count as present, so they are never
// frozen again.
func (ix *Index) openRoundStore() error {
	dir := ix.opt.RoundStore
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("round store: %w", err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("round store: %w", err)
	}
	var top int64 = -1
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		h, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || h <= 0 || strconv.FormatInt(h, 10) != e.Name() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name(), roundFile))
		if err != nil {
			continue // an interrupted freeze; it is redone
		}
		var fr FrozenRound
		if json.Unmarshal(raw, &fr) != nil || fr.Height != h {
			ix.frozen[h] = nil
		} else {
			ix.frozen[h] = &fr
		}
		if h > top {
			top = h
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dir, baselineFile)); err == nil {
		if b, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64); err == nil && b >= 0 {
			ix.frozenBase, ix.baseKnown = b, true
			return nil
		}
	}
	if top >= 0 {
		// Records without a baseline: never reach below what is stored.
		if err := ix.writeBaseline(top); err != nil {
			return err
		}
	}
	return nil
}

func (ix *Index) writeBaseline(h int64) error {
	if err := atomicWrite(filepath.Join(ix.opt.RoundStore, baselineFile), []byte(strconv.FormatInt(h, 10)+"\n")); err != nil {
		return fmt.Errorf("round store: %w", err)
	}
	ix.frozenBase, ix.baseKnown = h, true
	return nil
}

// syncFrozen freezes every solve above the baseline that has no record yet,
// oldest first. The first pass on an empty store only sets the baseline to
// the newest solve already in the log, so history from before the store
// existed is never frozen from possibly pruned sharelogs. A failure stops
// the pass; the next sync retries.
func (ix *Index) syncFrozen() {
	if ix.opt.RoundStore == "" || ix.opt.Root == "" {
		return
	}
	if !ix.baseKnown {
		var top int64
		for h, b := range ix.st.Blocks {
			if b.Status != StatusRejected && h > top {
				top = h
			}
		}
		_ = ix.writeBaseline(top)
		return
	}
	var due []int64
	for h, b := range ix.st.Blocks {
		if h <= ix.frozenBase || b.Time.IsZero() || (b.Status != StatusAccepted && b.Status != StatusOrphaned) {
			continue
		}
		if _, ok := ix.frozen[h]; ok {
			continue
		}
		due = append(due, h)
	}
	sort.Slice(due, func(i, j int) bool { return due[i] < due[j] })
	for _, h := range due {
		if err := ix.freeze(ix.st.Blocks[h]); err != nil {
			return
		}
	}
}

// acceptedBefore is the newest accepted block below height.
func (ix *Index) acceptedBefore(height int64) (h int64, at time.Time, ok bool) {
	for bh, b := range ix.st.Blocks {
		if bh >= height || b.Status != StatusAccepted {
			continue
		}
		if !ok || bh > h {
			h, at, ok = bh, b.Time, true
		}
	}
	return
}

// roundDirs lists the sharelog height directories of a round that started
// after the accepted block from (none when have is false) and closes at the
// solve at height to (inclusive; to < 0 means open-ended). The started-after
// block's own directory holds its round's work and is excluded.
func (ix *Index) roundDirs(from int64, have bool, to int64) []string {
	ents, err := os.ReadDir(ix.logsDir())
	if err != nil {
		return nil
	}
	type hd struct {
		h    int64
		name string
	}
	var dirs []hd
	for _, e := range ents {
		n := e.Name()
		if len(n) != 8 || !e.IsDir() || strings.ToLower(n) != n {
			continue
		}
		v, err := strconv.ParseInt(n, 16, 64)
		if err != nil || v < from || (have && v == from) || (to >= 0 && v > to) {
			continue
		}
		dirs = append(dirs, hd{v, n})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].h < dirs[j].h })
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = d.name
	}
	return out
}

// sharelogsIn lists the sharelog file names of one height directory.
func sharelogsIn(dir string) []string {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".sharelog") {
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)
	return names
}

// collectRound sums the accepted shares created in (since, until] from the
// round's sharelog directories. It returns the per-address figures and the
// files read, as "dir/file".
func (ix *Index) collectRound(from int64, have bool, since, until time.Time, to int64) (map[string]float64, map[string]AddrStat, []string) {
	sums, addrs := map[string]float64{}, map[string]AddrStat{}
	var files []string
	for _, dn := range ix.roundDirs(from, have, to) {
		dir := filepath.Join(ix.logsDir(), dn)
		for _, n := range sharelogsIn(dir) {
			raw, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				continue
			}
			files = append(files, dn+"/"+n)
			end := bytes.LastIndexByte(raw, '\n') + 1
			for _, line := range strings.Split(string(raw[:end]), "\n") {
				if line == "" {
					continue
				}
				sh, err := poolfmt.ParseSharelogLine(line)
				if err != nil || !sh.Result {
					continue
				}
				t, ok := shareTime(sh)
				if !ok || !t.After(since) || t.After(until) {
					continue
				}
				addRoundShare(sums, addrs, sh, t)
				if sh.SDiff > 0 {
					ix.noteBestMinute(t, sh.SDiff)
				}
			}
		}
	}
	return sums, addrs, files
}

// freeze builds the record of the round closed by solve b, archives its
// sharelogs and writes the record. An existing record is loaded, never
// replaced.
func (ix *Index) freeze(b *Block) error {
	dir := filepath.Join(ix.opt.RoundStore, strconv.FormatInt(b.Height, 10))
	path := filepath.Join(dir, roundFile)
	if raw, err := os.ReadFile(path); err == nil {
		var fr FrozenRound
		if json.Unmarshal(raw, &fr) == nil && fr.Height == b.Height {
			ix.frozen[b.Height] = &fr
		} else {
			ix.frozen[b.Height] = nil
		}
		return nil
	}
	prevH, prevT, have := ix.acceptedBefore(b.Height)
	sums, addrs, files := ix.collectRound(prevH, have, prevT, b.Time, b.Height)
	fr := FrozenRound{
		Height: b.Height, Hash: b.Hash, Status: b.Status, SolvedAt: b.Time,
		FinderWorker: b.Finder, FinderAddress: poolfmt.CanonicalAddress(FinderAddress(b.Finder)),
		PrevBlockHeight: prevH, StartedAt: prevT, FrozenAt: time.Now().UTC(),
	}
	for a, w := range sums {
		if w <= 0 {
			continue
		}
		st := addrs[a]
		fr.TotalWork += w
		fr.Addresses = append(fr.Addresses, FrozenAddress{Address: a, Username: st.Username, Work: w, Shares: st.Shares, LastShareAt: st.Last})
	}
	sort.Slice(fr.Addresses, func(i, j int) bool {
		if fr.Addresses[i].Work != fr.Addresses[j].Work {
			return fr.Addresses[i].Work > fr.Addresses[j].Work
		}
		return fr.Addresses[i].Address < fr.Addresses[j].Address
	})
	if fr.Addresses == nil {
		fr.Addresses = []FrozenAddress{}
	}
	if err := archiveSharelogs(ix.logsDir(), filepath.Join(dir, archiveDir), files); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(fr, "", "  ")
	if err != nil {
		return err
	}
	if err := writeNew(path, append(raw, '\n')); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	ix.frozen[b.Height] = &fr
	return nil
}

// archiveSharelogs copies each "dir/file" under src to the same relative
// path under dst.
func archiveSharelogs(src, dst string, files []string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	made := map[string]bool{}
	for _, rel := range files {
		d := filepath.Join(dst, filepath.Dir(rel))
		if !made[d] {
			if err := os.MkdirAll(d, 0o700); err != nil {
				return err
			}
			made[d] = true
		}
		if err := copyFile(filepath.Join(src, rel), filepath.Join(dst, rel)); err != nil {
			return err
		}
	}
	for d := range made {
		if err := syncDir(d); err != nil {
			return err
		}
	}
	return syncDir(dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// writeNew creates path with data and fails with os.ErrExist rather than
// replace an existing file: the data is written to a temp file, fsynced, and
// hard-linked into place.
func writeNew(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	err = os.Link(tmp, path)
	os.Remove(tmp)
	if err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// rewriteFrozen saves a record whose status or empty hash changed.
func (ix *Index) rewriteFrozen(fr *FrozenRound) error {
	raw, err := json.MarshalIndent(fr, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(ix.opt.RoundStore, strconv.FormatInt(fr.Height, 10), roundFile), append(raw, '\n'))
}

// Solves lists the frozen rounds above height since, oldest first.
func (ix *Index) Solves(since int64) []Solve {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var out []Solve
	for h, fr := range ix.frozen {
		if h <= since || fr == nil {
			continue
		}
		out = append(out, Solve{Height: h, Hash: fr.Hash, SolvedAt: fr.SolvedAt, Status: fr.Status})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Height < out[j].Height })
	return out
}

// FrozenRound returns the frozen record of a solve height.
func (ix *Index) FrozenRound(height int64) (FrozenRound, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	fr := ix.frozen[height]
	if fr == nil {
		return FrozenRound{}, false
	}
	return fr.clone(), true
}

// SetSolveStatus records the node's verdict on a solve, accepted or
// orphaned, in both the block table and the frozen record, and saves both.
// Marking the newest solve orphaned makes the live round fall back to the
// previous accepted block on the next sync, so the orphaned round's work
// carries into the current one; marking it accepted again undoes that.
func (ix *Index) SetSolveStatus(height int64, status string) error {
	if status != StatusAccepted && status != StatusOrphaned {
		return fmt.Errorf("poolindex: status %q is not accepted or orphaned", status)
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	b := ix.st.Blocks[height]
	fr := ix.frozen[height]
	if b == nil && fr == nil {
		return ErrUnknownSolve
	}
	ix.setStatusLocked(height, status)
	if err := ix.flush(); err != nil {
		return err
	}
	if fr != nil && fr.Status != status {
		next := fr.clone()
		next.Status = status
		if err := ix.rewriteFrozen(&next); err != nil {
			return err
		}
		ix.frozen[height] = &next
	}
	return nil
}

// setStatusLocked changes a known block's status and remembers orphan
// verdicts across rescans.
func (ix *Index) setStatusLocked(height int64, status string) {
	b := ix.st.Blocks[height]
	if b == nil || b.Status == status {
		return
	}
	b.Status = status
	ix.dirty.blocks[height] = true
	if status == StatusOrphaned {
		ix.orphans[height] = true
	} else {
		delete(ix.orphans, height)
	}
}

// fillFrozenHash sets the hash of a frozen record that was frozen without
// one. A hash already present is never changed.
func (ix *Index) fillFrozenHash(height int64, hash string) {
	fr := ix.frozen[height]
	if fr == nil || fr.Hash != "" || hash == "" {
		return
	}
	next := fr.clone()
	next.Hash = hash
	if ix.rewriteFrozen(&next) == nil {
		ix.frozen[height] = &next
	}
}

// VerifySolveHash records the hash the node reports for a solve it knows to
// be ours. It sets the hash on the block and on the frozen record, replacing
// a provisional one, once: after that the record is marked verified and
// later calls change nothing. It reports whether anything was written.
func (ix *Index) VerifySolveHash(height int64, hash string) (bool, error) {
	if hash == "" {
		return false, nil
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	b := ix.st.Blocks[height]
	fr := ix.frozen[height]
	if b == nil && fr == nil {
		return false, ErrUnknownSolve
	}
	changed := false
	if fr != nil && !fr.HashVerified {
		next := fr.clone()
		next.Hash, next.HashVerified = hash, true
		if err := ix.rewriteFrozen(&next); err != nil {
			return false, err
		}
		ix.frozen[height] = &next
		changed = true
	}
	if b != nil && b.Hash != hash && (fr == nil || changed) {
		b.Hash = hash
		ix.dirty.blocks[height] = true
		_ = ix.flush()
		changed = true
	}
	return changed, nil
}

// SetFrozenNode fills the node-sourced fields of a frozen record: the
// coinbase value and the finder and payroll outputs of the solved block. A
// record that already carries them is never changed. It reports whether the
// record was written.
func (ix *Index) SetFrozenNode(height, coinbaseSats int64, finder, payroll BlockOutput) (bool, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	fr := ix.frozen[height]
	if fr == nil {
		return false, ErrUnknownSolve
	}
	if fr.CoinbaseSats != nil {
		return false, nil
	}
	next := fr.clone()
	next.CoinbaseSats = &coinbaseSats
	next.FinderOutput = &finder
	next.PayrollOutput = &payroll
	if err := ix.rewriteFrozen(&next); err != nil {
		return false, err
	}
	ix.frozen[height] = &next
	return true, nil
}
