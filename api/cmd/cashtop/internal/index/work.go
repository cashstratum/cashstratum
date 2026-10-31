package index

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"cashstratumapi/internal/poolfmt"
)

func (ix *Index) logsDir() string { return filepath.Join(ix.opt.Root, "logs") }

// syncWork adds the accepted diff of new sharelog lines in the newest height
// directory to the per-address sums. A new height starts the sums over.
func (ix *Index) syncWork() {
	if ix.opt.Root == "" {
		return
	}
	h, dir, ok := ix.newestHeightDir()
	if !ok {
		return
	}
	s, d := ix.st, ix.dirty
	for attempt := 0; attempt < 2; attempt++ {
		if s.Work.Height != h {
			s.Work = Work{Height: h, Sums: map[string]float64{}, Files: map[string]int64{}}
			d.work, d.files, d.workRst = map[string]bool{}, map[string]bool{}, true
		}
		if ix.scanHeightDir(dir) {
			return
		}
		// A file shrank or vanished: the sums are no longer trustworthy.
		s.Work = Work{Height: -1, Sums: map[string]float64{}, Files: map[string]int64{}}
	}
}

func (ix *Index) newestHeightDir() (int64, string, bool) {
	ents, err := os.ReadDir(ix.logsDir())
	if err != nil {
		return 0, "", false
	}
	var best int64 = -1
	var name string
	for _, e := range ents {
		n := e.Name()
		if len(n) != 8 || !e.IsDir() {
			continue
		}
		v, err := strconv.ParseInt(n, 16, 64)
		if err != nil || strings.ToLower(n) != n {
			continue
		}
		if v > best {
			best, name = v, n
		}
	}
	if best < 0 {
		return 0, "", false
	}
	return best, filepath.Join(ix.logsDir(), name), true
}

// scanHeightDir reads the unread part of each sharelog file. It returns
// false when a file is smaller than what was already counted.
func (ix *Index) scanHeightDir(dir string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	var names []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".sharelog") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	s, d := ix.st, ix.dirty
	for _, n := range names {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		off := s.Work.Files[n]
		if st.Size() < off {
			f.Close()
			return false
		}
		if st.Size() == off {
			f.Close()
			continue
		}
		data := make([]byte, st.Size()-off)
		m, _ := f.ReadAt(data, off)
		f.Close()
		data = data[:m]
		end := bytes.LastIndexByte(data, '\n') + 1
		for _, line := range strings.Split(string(data[:end]), "\n") {
			if line == "" {
				continue
			}
			sh, err := poolfmt.ParseSharelogLine(line)
			if err != nil || !sh.Result {
				continue
			}
			addr := PayoutAddress(sh)
			if addr == "" {
				continue
			}
			s.Work.Sums[addr] += sh.Diff
			d.work[addr] = true
		}
		if end > 0 {
			s.Work.Files[n] = off + int64(end)
			d.files[n] = true
		}
	}
	return true
}

func signature(path string) (fileSig, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return fileSig{}, false
	}
	return fileSig{size: st.Size(), mod: st.ModTime().UnixNano(), ino: inodeOf(st)}, true
}

// syncSolved merges the solved-heights index and solves/INDEX.tsv into the
// block table. Both files are re-read whenever their identity changes, since
// the pool replaces them by rename.
func (ix *Index) syncSolved() {
	if ix.opt.Root == "" {
		return
	}
	idx := filepath.Join(ix.opt.Root, "solved-heights.idx")
	if sg, ok := signature(idx); ok && sg != ix.sigIdx {
		if raw, err := os.ReadFile(idx); err == nil {
			if hs, err := poolfmt.ParseSolvedHeights(bytes.NewReader(raw)); err == nil {
				ix.sigIdx = sg
				for _, h := range hs {
					if _, ok := ix.st.Blocks[h]; !ok {
						ix.st.Blocks[h] = &Block{Height: h, Status: StatusAccepted}
						ix.dirty.blocks[h] = true
					}
				}
			}
		}
	}
	tsv := filepath.Join(ix.opt.Root, "solves", "INDEX.tsv")
	if sg, ok := signature(tsv); ok && sg != ix.sigTSV {
		if raw, err := os.ReadFile(tsv); err == nil {
			if es, err := poolfmt.ParseSolvesIndex(bytes.NewReader(raw)); err == nil {
				ix.sigTSV = sg
				for _, e := range es {
					ix.mergeSolve(e)
				}
			}
		}
	}
}

func (ix *Index) mergeSolve(e poolfmt.SolveIndexEntry) {
	b := ix.st.Blocks[e.Height]
	changed := false
	if b == nil {
		status := StatusAccepted
		switch u := strings.ToUpper(e.Outcome); {
		case strings.Contains(u, "REJECT"):
			status = StatusRejected
		case strings.Contains(u, "ORPHAN"):
			status = StatusOrphaned
		}
		b = &Block{Height: e.Height, Status: status}
		ix.st.Blocks[e.Height] = b
		changed = true
	}
	if b.Hash == "" && e.Hash != "" {
		b.Hash, changed = e.Hash, true
	}
	if b.Finder == "" && e.Finder != "" {
		b.Finder, changed = e.Finder, true
	}
	if b.Time.IsZero() && e.HasTime {
		b.Time, changed = e.Time, true
	}
	if changed {
		ix.dirty.blocks[e.Height] = true
	}
}

func inodeOf(st os.FileInfo) uint64 { return inodeSys(st) }

// PayoutAddress is the address a share is credited to, without its network
// prefix: the sharelog's username, or the address part of the worker name
// when the username is empty. The sharelog's "address" field is the miner's
// connection address (an IP) and never identifies the payout.
func PayoutAddress(sh poolfmt.Share) string {
	a := sh.UserName
	if a == "" {
		a, _ = poolfmt.SplitWorker(sh.WorkerName)
	}
	return poolfmt.NormalizeAddress(a)
}

// lastAccepted is the pool's newest accepted block.
func (ix *Index) lastAccepted() (height int64, at time.Time, ok bool) {
	for h, b := range ix.st.Blocks {
		if b.Status != StatusAccepted {
			continue
		}
		if !ok || h > height {
			height, at, ok = h, b.Time, true
		}
	}
	return
}

// shareTime reads a sharelog createdate ("sec,nsec").
func shareTime(sh poolfmt.Share) (time.Time, bool) {
	sec, nsec, _ := strings.Cut(sh.CreateDate, ",")
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, false
	}
	ns, _ := strconv.ParseInt(nsec, 10, 64)
	return time.Unix(n, ns).UTC(), true
}

// RoundKey names a worker by payout address and worker name, never by the
// connection address.
func RoundKey(sh poolfmt.Share) string {
	_, rig := poolfmt.SplitWorker(sh.WorkerName)
	addr := PayoutAddress(sh)
	if addr == "" {
		return ""
	}
	if rig == "" {
		return addr
	}
	return addr + "." + rig
}

// syncRound keeps the best accepted share per worker since the pool's last
// accepted block. It reads the height directories from that block's height
// on, taking only shares newer than the block, and starts over when a
// different block becomes the latest.
func (ix *Index) syncRound() {
	if ix.opt.Root == "" {
		return
	}
	ents, err := os.ReadDir(ix.logsDir())
	if err != nil {
		return
	}
	s, d := ix.st, ix.dirty
	h, at, have := ix.lastAccepted()
	if !have {
		h = 0
	}
	if s.Round.Height != h || !s.Round.Since.Equal(at) {
		s.Round = newRound()
		s.Round.Height, s.Round.Since = h, at
		d.round = true
	}
	for k, b := range s.Round.Best {
		if !b.Time.After(s.Round.Since) {
			delete(s.Round.Best, k)
			d.round = true
		}
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
		if err != nil || v < h || (have && at.IsZero() && v == h) {
			continue
		}
		dirs = append(dirs, hd{v, n})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].h < dirs[j].h })
	if len(dirs) > 0 && !s.Round.Seen {
		s.Round.Seen, d.round = true, true
	}
	for _, dr := range dirs {
		dir := filepath.Join(ix.logsDir(), dr.name)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var names []string
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".sharelog") {
				names = append(names, f.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			ix.scanRoundFile(filepath.Join(dir, n), dr.name+"/"+n)
		}
	}
}

func (ix *Index) scanRoundFile(path, key string) {
	s, d := ix.st, ix.dirty
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	off := s.Round.Files[key]
	if st.Size() < off {
		off = 0 // truncated: read it again from the top
	}
	if st.Size() == off {
		return
	}
	data := make([]byte, st.Size()-off)
	m, _ := f.ReadAt(data, off)
	data = data[:m]
	end := bytes.LastIndexByte(data, '\n') + 1
	for _, line := range strings.Split(string(data[:end]), "\n") {
		if line == "" {
			continue
		}
		sh, err := poolfmt.ParseSharelogLine(line)
		if err != nil || !sh.Result || sh.SDiff <= 0 {
			continue
		}
		t, ok := shareTime(sh)
		if !ok || !t.After(s.Round.Since) {
			continue
		}
		k := RoundKey(sh)
		if k == "" {
			continue
		}
		if cur, ok := s.Round.Best[k]; !ok || sh.SDiff > cur.Diff {
			s.Round.Best[k] = BestShare{Address: k, Diff: sh.SDiff, Time: t}
			d.round = true
		}
	}
	if end > 0 {
		s.Round.Files[key] = off + int64(end)
		d.round = true
	}
}
