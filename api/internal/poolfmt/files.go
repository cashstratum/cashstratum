package poolfmt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Share is one sharelog record.
type Share struct {
	WorkInfoID  int64   `json:"workinfoid"`
	ClientID    int64   `json:"clientid"`
	Enonce1     string  `json:"enonce1"`
	Nonce2      string  `json:"nonce2"`
	Nonce       string  `json:"nonce"`
	NTime       string  `json:"ntime"`
	Diff        float64 `json:"diff"`
	SDiff       float64 `json:"sdiff"`
	Hash        string  `json:"hash"`
	Result      bool    `json:"result"`
	Error       string  `json:"error"`
	Errn        int64   `json:"errn"`
	CreateDate  string  `json:"createdate"`
	CreateBy    string  `json:"createby"`
	CreateCode  string  `json:"createcode"`
	CreateInet  string  `json:"createinet"`
	WorkerName  string  `json:"workername"`
	UserName    string  `json:"username"`
	Address     string  `json:"address"`
	Agent       string  `json:"agent"`
	VersionMask string  `json:"version_mask"`
}

// Accepted reports whether the share was accepted.
func (s Share) Accepted() bool { return s.Result }

// IsBlock reports whether the share solves a block at the given network
// difficulty: it was accepted and its difficulty reaches the network's.
func (s Share) IsBlock(networkDiff float64) bool {
	return s.Result && networkDiff > 0 && s.SDiff >= networkDiff
}

// ParseSharelogLine parses one compact JSON sharelog line.
func ParseSharelogLine(line string) (Share, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Share{}, ErrEmpty
	}
	var s Share
	if err := json.Unmarshal([]byte(line), &s); err != nil {
		return Share{}, err
	}
	return s, nil
}

var reShareFile = regexp.MustCompile(`^([0-9a-f]{16})\.sharelog$`)
var reHex8 = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ParseSharelogName decodes logs/<%08x height>/<%016x workinfoid>.sharelog.
func ParseSharelogName(dir, file string) (height int64, workinfoID uint64, ok bool) {
	m := reShareFile.FindStringSubmatch(file)
	if m == nil || !reHex8.MatchString(dir) {
		return 0, 0, false
	}
	h, err := strconv.ParseInt(dir, 16, 64)
	if err != nil {
		return 0, 0, false
	}
	w, err := strconv.ParseUint(m[1], 16, 64)
	if err != nil {
		return 0, 0, false
	}
	return h, w, true
}

func lineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(io.LimitReader(r, maxFile))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	return sc
}

func scanErr(sc *bufio.Scanner) error {
	if err := sc.Err(); err != nil {
		if err == bufio.ErrTooLong {
			return ErrTooLong
		}
		return err
	}
	return nil
}

// ParseSolvedHeights parses solved-heights.idx: a marker line followed by
// sorted %08x heights. Lines that are not eight lowercase hex digits are
// skipped. An empty file is an error so a torn read is not mistaken for none.
func ParseSolvedHeights(r io.Reader) ([]int64, error) {
	if r == nil {
		return nil, ErrEmpty
	}
	sc := lineScanner(r)
	var out []int64
	seen := false
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" {
			continue
		}
		seen = true
		if !reHex8.MatchString(l) {
			continue
		}
		if h, err := strconv.ParseInt(l, 16, 64); err == nil {
			out = append(out, h)
		}
	}
	if err := scanErr(sc); err != nil {
		return out, err
	}
	if !seen {
		return nil, ErrEmpty
	}
	return out, nil
}

// SolveHeader is the "# key: value" header of a solves/<height>-<hash>.log file.
type SolveHeader struct {
	Preserved     string
	Source        string
	Height        int64
	Hash          string
	ZMQHash       string // empty when "none"
	Finder        string
	Confirmations Opt[int64]
	Outcome       string // ACCEPTED, HASH_MISMATCH or ORPHANED
	OutcomeDetail string
	Window        string
}

var (
	reHeaderKV   = regexp.MustCompile(`^# (\w+):\s+(.*)$`)
	reHeaderHash = regexp.MustCompile(`^(\S+)(?: \(ZMQ-seen: (\S+)\))?`)
	reHeaderChn  = regexp.MustCompile(`^confirmations=(-?\d+) at preserve time -> ([A-Z_]+)(?:\((.*)\))?`)
	reSolveFile  = regexp.MustCompile(`^(\d+)-([0-9a-f]{64})\.log$`)
)

// ParseSolveFilename decodes <height>-<hash>.log.
func ParseSolveFilename(name string) (height int64, hash string, ok bool) {
	m := reSolveFile.FindStringSubmatch(name)
	if m == nil {
		return 0, "", false
	}
	h, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return h, m[2], true
}

// ParseSolveHeader reads the leading comment header of a solve file and stops
// at the first non-comment line.
func ParseSolveHeader(r io.Reader) (SolveHeader, error) {
	var h SolveHeader
	if r == nil {
		return h, ErrEmpty
	}
	sc := lineScanner(r)
	n, got := 0, false
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if !strings.HasPrefix(l, "#") {
			break
		}
		if n++; n > 64 {
			break
		}
		if i := strings.Index(l, "preserved "); i >= 0 && n == 1 {
			h.Preserved = strings.TrimSpace(l[i+len("preserved "):])
			continue
		}
		m := reHeaderKV.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[2])
		switch m[1] {
		case "source":
			h.Source = v
		case "height":
			if x, ok := i64(v); ok {
				h.Height, got = x, true
			}
		case "hash":
			if hm := reHeaderHash.FindStringSubmatch(v); hm != nil {
				h.Hash = hm[1]
				if hm[2] != "none" {
					h.ZMQHash = hm[2]
				}
			}
		case "finder":
			h.Finder = v
		case "chain":
			if cm := reHeaderChn.FindStringSubmatch(v); cm != nil {
				if x, ok := i64(cm[1]); ok {
					h.Confirmations = some(x)
				}
				h.Outcome, h.OutcomeDetail = cm[2], cm[3]
			}
		case "window":
			h.Window = v
		}
	}
	if err := scanErr(sc); err != nil {
		return h, err
	}
	if !got {
		return SolveHeader{}, fmt.Errorf("poolfmt: no height in solve header")
	}
	return h, nil
}

// SolveIndexEntry is one row of solves/INDEX.tsv.
type SolveIndexEntry struct {
	Height        int64
	Hash          string
	Time          time.Time
	HasTime       bool
	Outcome       string
	Finder        string
	Confirmations Opt[int64]
	ContextFile   string
}

// ParseSolvesIndex parses solves/INDEX.tsv. The header row and malformed rows
// are skipped. Timestamps are host-local, like the log prefix.
func ParseSolvesIndex(r io.Reader) ([]SolveIndexEntry, error) {
	return ParseSolvesIndexIn(r, nil)
}

// ParseSolvesIndexIn is ParseSolvesIndex with an explicit time zone.
func ParseSolvesIndexIn(r io.Reader, loc *time.Location) ([]SolveIndexEntry, error) {
	if r == nil {
		return nil, ErrEmpty
	}
	sc := lineScanner(r)
	var out []SolveIndexEntry
	for sc.Scan() {
		f := strings.Split(strings.TrimRight(sc.Text(), "\r"), "\t")
		if len(f) < 6 {
			continue
		}
		h, ok := i64(f[0])
		if !ok {
			continue
		}
		e := SolveIndexEntry{Height: h, Hash: f[1], Outcome: f[3], Finder: f[4]}
		e.Time, e.HasTime = ParseStamp(f[2], loc)
		if c, ok := i64(f[5]); ok {
			e.Confirmations = some(c)
		}
		if len(f) > 6 {
			e.ContextFile = f[6]
		}
		out = append(out, e)
	}
	return out, scanErr(sc)
}
