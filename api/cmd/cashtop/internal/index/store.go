package index

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cashstratumapi/cmd/cashtop/internal/tail"
)

// SchemaVersion changes when the persisted layout changes; ParserVersion
// changes when the log parsing changes what the index would derive. Either
// bump forces a rescan.
const (
	SchemaVersion = 1
	ParserVersion = 4
)

const (
	snapshotName = "state.json"
	journalName  = "journal.jsonl"
	compactBytes = 1 << 20
)

type snapshot struct {
	Schema int    `json:"schema"`
	Parser int    `json:"parser"`
	Gen    int    `json:"gen"`
	State  *State `json:"state"`
}

type seriesDelta struct {
	From   int     `json:"from"`
	Points []Point `json:"points"`
}

type workDelta struct {
	Height int64              `json:"height"`
	Reset  bool               `json:"reset,omitempty"`
	Sums   map[string]float64 `json:"sums,omitempty"`
	Files  map[string]int64   `json:"files,omitempty"`
}

type transient struct {
	LastZMQ    string    `json:"last_zmq"`
	LastReject string    `json:"last_reject"`
	OpenSolve  int64     `json:"open_solve"`
	Solved     time.Time `json:"solved,omitzero"`
	Tip        int64     `json:"tip,omitempty"`
}

// rec is one journal line. "cur" closes a batch: records before it only
// count once the cursor record that follows has been read.
type rec struct {
	K      string       `json:"k"`
	Schema int          `json:"schema,omitempty"`
	Parser int          `json:"parser,omitempty"`
	Gen    int          `json:"gen,omitempty"`
	Block  *Block       `json:"block,omitempty"`
	Worker *Worker      `json:"worker,omitempty"`
	Best   []BestShare  `json:"best,omitempty"`
	HR     *seriesDelta `json:"hr,omitempty"`
	Luck   *seriesDelta `json:"luck,omitempty"`
	Work   *workDelta   `json:"work,omitempty"`
	Round  *Round       `json:"round,omitempty"`
	Tr     *transient   `json:"tr,omitempty"`
	Cur    *tail.Cursor `json:"cur,omitempty"`
}

var errCorrupt = errors.New("index: corrupt cache")

// atomicWrite replaces path with data: write a same-directory temp file,
// fsync it, rename over the target, fsync the directory.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// load reads the snapshot and replays the journal. Any error means the
// caller must rescan.
func (ix *Index) load() (gen int, err error) {
	raw, err := os.ReadFile(filepath.Join(ix.dir, snapshotName))
	if err != nil {
		return 0, err
	}
	var sn snapshot
	if err := json.Unmarshal(raw, &sn); err != nil || sn.State == nil {
		return 0, errCorrupt
	}
	if sn.Schema != SchemaVersion || sn.Parser != ParserVersion {
		return 0, fmt.Errorf("version change (schema %d parser %d)", sn.Schema, sn.Parser)
	}
	st := sn.State
	if st.Blocks == nil {
		st.Blocks = map[int64]*Block{}
	}
	if st.Workers == nil {
		st.Workers = map[string]*Worker{}
	}
	if st.Work.Sums == nil {
		st.Work.Sums = map[string]float64{}
	}
	if st.Round.Files == nil {
		st.Round.Files = map[string]int64{}
	}
	if st.Round.Best == nil {
		st.Round.Best = map[string]BestShare{}
	}
	if st.Work.Files == nil {
		st.Work.Files = map[string]int64{}
	}
	ix.st = st
	jr, err := os.ReadFile(filepath.Join(ix.dir, journalName))
	if err != nil {
		if os.IsNotExist(err) {
			return sn.Gen, nil
		}
		return 0, err
	}
	clean, err := ix.replay(jr, sn.Gen)
	if err != nil {
		return 0, err
	}
	if clean {
		// Keep appending to the journal that was just replayed.
		if f, err := os.OpenFile(filepath.Join(ix.dir, journalName), os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			ix.journal, ix.jbytes = f, int64(len(jr))
		}
	}
	return sn.Gen, nil
}

// replay applies the journal's committed batches. clean reports whether the
// journal can be appended to as it stands; a torn tail or an older
// generation makes the caller write a fresh one instead.
func (ix *Index) replay(data []byte, gen int) (clean bool, err error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var batch []rec
	first := true
	for sc.Scan() {
		line := sc.Bytes()
		var r rec
		if err := json.Unmarshal(line, &r); err != nil {
			// A torn final line is a crash mid-append; anything else is damage.
			if !bytes.HasSuffix(data, []byte("\n")) && bytes.HasSuffix(data, line) {
				return false, nil
			}
			return false, errCorrupt
		}
		if first {
			first = false
			if r.K != "hdr" || r.Schema != SchemaVersion || r.Parser != ParserVersion {
				return false, errCorrupt
			}
			if r.Gen != gen {
				return false, nil // journal predates the snapshot
			}
			continue
		}
		batch = append(batch, r)
		if r.K == "cur" {
			for _, b := range batch {
				ix.applyRec(b)
			}
			batch = batch[:0]
		}
	}
	if err := sc.Err(); err != nil {
		return false, errCorrupt
	}
	return !first && len(batch) == 0, nil
}

func (ix *Index) applyRec(r rec) {
	s := ix.st
	switch r.K {
	case "block":
		b := *r.Block
		s.Blocks[b.Height] = &b
	case "worker":
		w := *r.Worker
		s.Workers[w.Name] = &w
	case "best":
		s.Best = r.Best
	case "hr":
		s.Hashrate = applySeries(s.Hashrate, r.HR)
	case "luck":
		s.PoolLuck = applySeries(s.PoolLuck, r.Luck)
	case "work":
		w := r.Work
		if w.Reset || w.Height != s.Work.Height {
			s.Work = Work{Height: w.Height, Sums: map[string]float64{}, Files: map[string]int64{}}
		}
		for k, v := range w.Sums {
			s.Work.Sums[k] = v
		}
		for k, v := range w.Files {
			s.Work.Files[k] = v
		}
	case "tr":
		s.LastZMQ, s.LastReject, s.OpenSolve, s.Solved, s.Tip = r.Tr.LastZMQ, r.Tr.LastReject, r.Tr.OpenSolve, r.Tr.Solved, r.Tr.Tip
	case "round":
		s.Round = *r.Round
		if s.Round.Files == nil {
			s.Round.Files = map[string]int64{}
		}
		if s.Round.Best == nil {
			s.Round.Best = map[string]BestShare{}
		}
	case "cur":
		s.Cursor = *r.Cur
	}
}

func applySeries(s []Point, d *seriesDelta) []Point {
	if d.From > len(s) {
		d.From = len(s)
	}
	return append(s[:d.From], d.Points...)
}

// flush appends the pending changes and the cursor to the journal, and
// compacts into a fresh snapshot once the journal is large.
func (ix *Index) flush() error {
	if ix.dir == "" {
		ix.dirty = newDirty(ix.st)
		return nil
	}
	s, d := ix.st, ix.dirty
	if d.empty() && s.Cursor == ix.written {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	emit := func(r rec) { _ = enc.Encode(r) }
	for h := range d.blocks {
		emit(rec{K: "block", Block: s.Blocks[h]})
	}
	for n := range d.workers {
		emit(rec{K: "worker", Worker: s.Workers[n]})
	}
	if d.best {
		emit(rec{K: "best", Best: s.Best})
	}
	if d.hrFrom >= 0 {
		emit(rec{K: "hr", HR: &seriesDelta{From: d.hrFrom, Points: s.Hashrate[d.hrFrom:]}})
	}
	if d.luckFrom >= 0 {
		emit(rec{K: "luck", Luck: &seriesDelta{From: d.luckFrom, Points: s.PoolLuck[d.luckFrom:]}})
	}
	if len(d.work) > 0 || len(d.files) > 0 || d.workRst {
		w := &workDelta{Height: s.Work.Height, Reset: d.workRst, Sums: map[string]float64{}, Files: map[string]int64{}}
		for a := range d.work {
			w.Sums[a] = s.Work.Sums[a]
		}
		for f := range d.files {
			w.Files[f] = s.Work.Files[f]
		}
		emit(rec{K: "work", Work: w})
	}
	if d.round {
		emit(rec{K: "round", Round: &s.Round})
	}
	if d.trans {
		emit(rec{K: "tr", Tr: &transient{s.LastZMQ, s.LastReject, s.OpenSolve, s.Solved, s.Tip}})
	}
	c := s.Cursor
	emit(rec{K: "cur", Cur: &c})

	if ix.journal == nil {
		if err := ix.compact(); err != nil {
			return err
		}
	} else {
		n, err := ix.journal.Write(buf.Bytes())
		ix.jbytes += int64(n)
		if err != nil {
			return err
		}
		if err := ix.journal.Sync(); err != nil {
			return err
		}
	}
	ix.dirty = newDirty(s)
	ix.written = s.Cursor
	if ix.jbytes > compactBytes {
		return ix.compact()
	}
	return nil
}

// compact writes a snapshot of the whole state, then starts a new journal
// for the next generation. A crash between the two leaves a journal whose
// generation is older than the snapshot, which load ignores.
func (ix *Index) compact() error {
	s := ix.st
	if len(s.Hashrate) > seriesMax {
		s.Hashrate = append([]Point(nil), s.Hashrate[len(s.Hashrate)-seriesMax:]...)
	}
	if len(s.PoolLuck) > seriesMax {
		s.PoolLuck = append([]Point(nil), s.PoolLuck[len(s.PoolLuck)-seriesMax:]...)
	}
	ix.gen++
	raw, err := json.Marshal(snapshot{Schema: SchemaVersion, Parser: ParserVersion, Gen: ix.gen, State: s})
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(ix.dir, snapshotName), raw); err != nil {
		return err
	}
	hdr, _ := json.Marshal(rec{K: "hdr", Schema: SchemaVersion, Parser: ParserVersion, Gen: ix.gen})
	hdr = append(hdr, '\n')
	jp := filepath.Join(ix.dir, journalName)
	if ix.journal != nil {
		ix.journal.Close()
		ix.journal = nil
	}
	if err := atomicWrite(jp, hdr); err != nil {
		return err
	}
	f, err := os.OpenFile(jp, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	ix.journal = f
	ix.jbytes = int64(len(hdr))
	ix.dirty = newDirty(s)
	ix.written = s.Cursor
	return nil
}
