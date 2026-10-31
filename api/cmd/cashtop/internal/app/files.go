package app

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"cashstratumapi/cmd/cashtop/internal/index"
	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/poolfmt"
)

// fileStats is the last good state of the pool's stats files.
type fileStats struct {
	pool    poolfmt.PoolStatus
	hasPool bool
	users   map[string]poolfmt.UserFile // keyed by file name (the address)

	shareHeight int64
	agents      map[string]string  // worker -> useragent, current height
	diffs       map[string]float64 // worker -> latest share difficulty
	lastShare   map[string]time.Time
	recent      []poolfmt.Share // newest sharelog lines of the current height, oldest first
	noSharelogs bool
}

// readRetry parses path, retrying once when the read is torn or empty.
func readRetry[T any](path string, parse func(io.Reader) (T, error), ok func(T) bool) (T, error) {
	var zero T
	var err error
	for try := 0; try < 2; try++ {
		var data []byte
		data, err = os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return zero, err
			}
			continue
		}
		var v T
		v, err = parse(bytes.NewReader(data))
		if err == nil && ok(v) {
			return v, nil
		}
		if err == nil {
			err = fmt.Errorf("%s: incomplete", filepath.Base(path))
		}
		time.Sleep(20 * time.Millisecond)
	}
	return zero, err
}

// refreshFiles rereads users/*, pool.status and the newest sharelog
// directory. Failed reads keep the previous values.
func (a *App) refreshFiles() {
	logs := filepath.Join(a.cfg.Root, "logs")
	a.mu.Lock()
	st := a.stats
	a.mu.Unlock()

	if p, err := readRetry(filepath.Join(logs, "pool", "pool.status"), poolfmt.ParsePoolStatus,
		func(p poolfmt.PoolStatus) bool { return p.HasStatus || p.HasHashrate || p.HasShares }); err == nil {
		st.pool, st.hasPool = p, true
	}

	users := map[string]poolfmt.UserFile{}
	if ents, err := os.ReadDir(filepath.Join(logs, "users")); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			u, err := readRetry(filepath.Join(logs, "users", e.Name()), poolfmt.ParseUserFile,
				func(poolfmt.UserFile) bool { return true })
			if err != nil {
				if old, ok := st.users[e.Name()]; ok {
					users[e.Name()] = old
				}
				continue
			}
			users[e.Name()] = u
		}
		st.users = users
	}

	h, dir := newestShareDir(logs)
	st.noSharelogs = dir == ""
	if dir != "" {
		st.shareHeight = h
		st.agents, st.diffs, st.lastShare = map[string]string{}, map[string]float64{}, map[string]time.Time{}
		st.recent = nil
		files, _ := filepath.Glob(filepath.Join(dir, "*.sharelog"))
		for _, f := range files {
			scanTail(f, 256<<10, func(s poolfmt.Share) {
				st.recent = append(st.recent, s)
				w := s.WorkerName
				if s.Agent != "" {
					st.agents[w] = s.Agent
				}
				if s.Diff > 0 {
					st.diffs[w] = s.Diff
				}
				if t, ok := shareTime(s); ok && t.After(st.lastShare[w]) {
					st.lastShare[w] = t
				}
			})
		}
	}
	sort.SliceStable(st.recent, func(i, j int) bool {
		ti, _ := shareTime(st.recent[i])
		tj, _ := shareTime(st.recent[j])
		return ti.Before(tj)
	})
	if n := len(st.recent); n > maxRecent {
		st.recent = append([]poolfmt.Share(nil), st.recent[n-maxRecent:]...)
	}
	a.mu.Lock()
	a.stats = st
	a.mu.Unlock()
}

// maxRecent bounds the shares kept between refreshes.
const maxRecent = 4000

// Address implements ui.AddressSource from the current height's sharelogs.
func (a *App) Address(addr string) ui.AddressData {
	key := poolfmt.NormalizeAddress(addr)
	v := a.ix.Snapshot()
	a.mu.Lock()
	st := a.stats
	a.mu.Unlock()
	d := ui.AddressData{Height: st.shareHeight, LogShares: !st.noSharelogs, LastShare: map[string]time.Time{}}
	var sum float64
	for _, sh := range st.recent {
		if index.PayoutAddress(sh) != key {
			continue
		}
		d.Submits = append(d.Submits, sh)
		if sh.Result {
			sum += sh.Diff
		}
		if t, ok := shareTime(sh); ok && t.After(d.LastShare[sh.WorkerName]) {
			d.LastShare[sh.WorkerName] = t
		}
	}
	if n := len(d.Submits); n > 50 {
		d.Submits = d.Submits[n-50:]
	}
	// The index sums the whole height directory; the tail sum only covers
	// the moment before the index has seen this address at this height.
	d.Work = sum
	if w, ok := v.Work.Sums[key]; ok && v.Work.Height == st.shareHeight {
		d.Work = w
	}
	return d
}

// newestShareDir finds the highest %08x height directory under logs.
func newestShareDir(logs string) (int64, string) {
	ents, err := os.ReadDir(logs)
	if err != nil {
		return 0, ""
	}
	var best int64 = -1
	dir := ""
	for _, e := range ents {
		if !e.IsDir() || len(e.Name()) != 8 {
			continue
		}
		h, err := strconv.ParseInt(e.Name(), 16, 64)
		if err != nil {
			continue
		}
		if h > best {
			best, dir = h, filepath.Join(logs, e.Name())
		}
	}
	return best, dir
}

// scanTail parses the last limit bytes of a sharelog.
func scanTail(path string, limit int64, fn func(poolfmt.Share)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > limit {
		if _, err := f.Seek(fi.Size()-limit, io.SeekStart); err != nil {
			return
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if s, err := poolfmt.ParseSharelogLine(sc.Text()); err == nil {
			fn(s)
		}
	}
}

// shareTime reads createdate ("sec,nsec").
func shareTime(s poolfmt.Share) (time.Time, bool) {
	sec, nsec, _ := bytes.Cut([]byte(s.CreateDate), []byte(","))
	n, err := strconv.ParseInt(string(sec), 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, false
	}
	ns, _ := strconv.ParseInt(string(nsec), 10, 64)
	return time.Unix(n, ns).UTC(), true
}

// maxDrillShares bounds the drill-down for heights with very busy sharelogs.
const maxDrillShares = 20000

// DrillDown implements ui.Provider: the sharelog jobs of one height. A
// missing directory is not an error; the view explains it.
func (a *App) DrillDown(height int) (ui.DrillDown, error) {
	name := fmt.Sprintf("%08x", height)
	nd := a.networkDiff()
	d := ui.DrillDown{Height: height, Dir: name, NetworkDiff: nd}
	dir := filepath.Join(a.cfg.Root, "logs", name)
	files, err := filepath.Glob(filepath.Join(dir, "*.sharelog"))
	if err != nil || len(files) == 0 {
		return d, nil
	}
	found := false
	for _, b := range a.ix.Snapshot().Blocks {
		if b.Height == int64(height) {
			found = true
		}
	}
	type pos struct{ j, s int }
	var best pos
	bestSD := -1.0
	total := 0
	sort.Strings(files)
	for _, f := range files {
		_, wid, ok := poolfmt.ParseSharelogName(name, filepath.Base(f))
		id := strconv.FormatUint(wid, 16)
		if !ok {
			id = filepath.Base(f)
		}
		job := ui.DrillJob{ID: id}
		scanTail(f, 32<<20, func(s poolfmt.Share) {
			if total >= maxDrillShares && !s.IsBlock(nd) {
				return
			}
			total++
			t, _ := shareTime(s)
			job.Shares = append(job.Shares, ui.DrillShare{Time: t, Worker: s.WorkerName, Agent: s.Agent,
				Diff: s.Diff, SDiff: s.SDiff, Result: s.Result, Error: s.Error, Winning: s.IsBlock(nd)})
		})
		d.Jobs = append(d.Jobs, job)
	}
	for j, job := range d.Jobs {
		for i, s := range job.Shares {
			if s.Result && s.SDiff > bestSD {
				bestSD, best = s.SDiff, pos{j, i}
			}
		}
	}
	// Difficulty moves every block, so for a height the pool found, the
	// strongest accepted share is the solve even if today's figure is higher.
	if found && bestSD > 0 {
		d.Jobs[best.j].Shares[best.s].Winning = true
	}
	return d, nil
}
