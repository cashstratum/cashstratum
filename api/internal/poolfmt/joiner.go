package poolfmt

import "strings"

const (
	// ChunkBody is the longest message body the pool writes as one log line.
	// The logger splits anything longer into several lines of this many bytes
	// (DEFLOGBUFSIZ - 2 in src/libckpool.h).
	ChunkBody = 510
	// JoinGapMs is the largest timestamp gap, in milliseconds, between a full
	// chunk and the line that continues it.
	JoinGapMs = 50
)

// Joined is one logical log line produced by a Joiner.
type Joined struct {
	Line   string // the rejoined line, stamped like its first chunk
	Offset int64  // byte offset of the first chunk
}

// Joiner reassembles messages the pool split across several log lines. It
// applies the same rule as monitor.sh: a line whose body is exactly
// ChunkBody bytes is held; the next line continues it when it is stamped at
// most JoinGapMs after the previous chunk, and its body is appended. The
// message ends at the first continuation shorter than ChunkBody.
//
// A Joiner is not safe for concurrent use.
type Joiner struct {
	pend    string
	pendOff int64
	pendTS  string
	held    bool
}

// Pending reports whether a possibly incomplete line is being held, and the
// offset of its first chunk.
func (j *Joiner) Pending() (offset int64, ok bool) { return j.pendOff, j.held }

// Feed takes the next raw line (without its newline) and the offset where it
// starts, and returns the joined lines that are now complete, in order.
func (j *Joiner) Feed(line string, offset int64) []Joined {
	var out []Joined
	ts := stampOf(line)
	if j.held {
		gap := -1
		if ts != "" {
			a, aok := stampMs(ts)
			b, bok := stampMs(j.pendTS)
			if aok && bok {
				gap = a - b
			}
		}
		if gap >= 0 && gap <= JoinGapMs {
			j.pendTS = ts
			c := line[len(ts)+1:]
			j.pend += c
			if len(c) == ChunkBody {
				return nil
			}
			out = append(out, Joined{j.pend, j.pendOff})
			j.reset()
			return out
		}
		out = append(out, Joined{j.pend, j.pendOff})
		j.reset()
	}
	if ts != "" && len(line)-len(ts)-1 == ChunkBody {
		j.pend, j.pendOff, j.pendTS, j.held = line, offset, ts, true
		return out
	}
	return append(out, Joined{line, offset})
}

// Flush releases a held line, for use when the log has gone quiet.
func (j *Joiner) Flush() []Joined {
	if !j.held {
		return nil
	}
	r := Joined{j.pend, j.pendOff}
	j.reset()
	return []Joined{r}
}

func (j *Joiner) reset() { j.pend, j.pendTS, j.held = "", "", false }

// stampOf returns the leading "[...]" of a line, or "" when there is none.
func stampOf(s string) string {
	if !strings.HasPrefix(s, "[") {
		return ""
	}
	i := strings.Index(s, "] ")
	if i < 0 {
		return ""
	}
	return s[:i+1]
}

// stampMs converts "[YYYY-MM-DD HH:MM:SS.mmm]" to milliseconds since the
// start of the day. Day changes are not considered, matching monitor.sh.
func stampMs(t string) (int, bool) {
	if len(t) < 24 {
		return 0, false
	}
	d := func(i, n int) (int, bool) {
		v := 0
		for k := i; k < i+n; k++ {
			if t[k] < '0' || t[k] > '9' {
				return 0, false
			}
			v = v*10 + int(t[k]-'0')
		}
		return v, true
	}
	h, ok1 := d(12, 2)
	m, ok2 := d(15, 2)
	s, ok3 := d(18, 2)
	ms, ok4 := d(21, 3)
	if !(ok1 && ok2 && ok3 && ok4) {
		return 0, false
	}
	return ((h*60+m)*60+s)*1000 + ms, true
}
