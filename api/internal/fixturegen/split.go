package fixturegen

import "strings"

// chunkBody is the longest message body the pool writes as one log line: its
// logger copies DEFLOGBUFSIZ - 2 bytes per call (LOGMSGSIZ in
// src/libckpool.h, 512 - 2) and logs each piece as its own stamped line.
const chunkBody = 510

// SplitLine splits one generated line (stamp, body, trailing newline) the way
// the pool's logger does: the body is cut into pieces of at most 510 bytes and
// every piece is written with the same stamp. A line whose body fits is
// returned unchanged.
func SplitLine(line string) []string {
	nl := ""
	body := line
	if strings.HasSuffix(body, "\n") {
		nl, body = "\n", body[:len(body)-1]
	}
	i := strings.Index(body, "] ")
	if i < 0 || len(body)-(i+2) <= chunkBody {
		return []string{line}
	}
	stamp, msg := body[:i+2], body[i+2:]
	var out []string
	for len(msg) > 0 {
		n := min(len(msg), chunkBody)
		out = append(out, stamp+msg[:n]+nl)
		msg = msg[n:]
	}
	return out
}

// SetSplit makes Simulate and the bulk writer emit long lines as the pool
// does, in several stamped pieces. It is off by default.
func (g *Generator) SetSplit(on bool) { g.split = on }

func (g *Generator) splitLines(ls []string) []string {
	if !g.split {
		return ls
	}
	var out []string
	for _, l := range ls {
		out = append(out, SplitLine(l)...)
	}
	return out
}

// SetVerbose makes User lines carry the reject statistics a busy pool
// reports, which pushes them past the 510-byte limit. It is off by default so
// existing fixtures are unchanged.
func (g *Generator) SetVerbose(on bool) { g.verbose = on }
