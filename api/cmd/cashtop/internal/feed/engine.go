package feed

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Style classifies a row for theming. Code carries the exact SGR parameters
// monitor.sh would use, for callers that want them.
type Style int

const (
	StylePlain Style = iota
	StyleAddr        // per-address colour, see Row.AddrColour
	StyleConnect
	StyleDrop
	StyleFlap
	StylePool
	StylePoolAlert
	StyleDim
	StyleBlockBad
	StyleBlockGood
	StyleNewBlock
	StyleNetDiff
	StyleError
)

// Row is one visible feed line.
type Row struct {
	Time       time.Time // zero when the log line carried no timestamp
	Offset     int64     // byte offset of the (first) source line
	Stamp      string    // HH:MM:SS prefix as printed, may be empty
	Style      Style
	Code       string // SGR parameters, empty for plain
	AddrColour int    // palette index for StyleAddr rows, otherwise -1
	Text       string // visible text, identical to monitor.sh output
}

// Group labels and colours one address.
type Group struct {
	Label  string
	Colour string // SGR colour parameters, empty when none was given
}

// Config tunes the engine. The zero value is a valid default configuration.
type Config struct {
	FlapSecs   int              // sessions shorter than this are flaps; 0 means 5
	NoFlap     bool             // disable flap folding entirely
	Groups     map[string]Group // keyed by address without network prefix
	MaxTracked int              // bound for per-client maps; 0 means 4096
}

const (
	defaultFlapSecs = 5
	defaultTracked  = 4096
	joinChunk       = 510
	joinGapMs       = 50
)

// Palette is the 24-colour address palette (256-colour codes).
var Palette = [24]int{39, 45, 51, 75, 81, 111, 117, 141, 147, 171, 177, 183, 207, 213, 219, 214, 220, 226, 190, 154, 118, 84, 49, 43}

var (
	reUser     = regexp.MustCompile(`^(User|Worker) [^ ]+:\{`)
	rePassword = regexp.MustCompile(`^Client [^ ]+ set difficulty to [0-9]+ via password parameter`)
	reAgent    = regexp.MustCompile(`detected from useragent .* for client [^ ]+, applied difficulty`)
	rePattern  = regexp.MustCompile(`^Client [^ ]+ workername .* matched pattern .* applied difficulty`)
	reAuth     = regexp.MustCompile(`^Authorised client [^ ]+ [^ ]+ worker `)
	reReason   = regexp.MustCompile(` reason=[^ ]+`)
	reAge      = regexp.MustCompile(` age=[0-9.]+`)
	reBlockBad = regexp.MustCompile(`SUBMIT BLOCK RETURNED|SUBMIT BLOCK GOT NO RESPONSE|had block [0-9]+ rejected|block submit rejected`)
	reBlockOK  = regexp.MustCompile(`BLOCK ACCEPTED|Solved and confirmed|Possible block solve|Block solved after|Submitting (possible )?block`)
	reError    = regexp.MustCompile(`[Rr]eject|[Ee]rror|ERROR|[Ff]ail|CRITICAL|Unable|[Ii]nvalid|rate limited`)
)

// Engine holds the cross-line state of the feed rules.
type Engine struct {
	flap    int
	noFlap  bool
	groups  map[string]Group
	tags    *capMap[string]
	api     *capMap[bool]
	authAt  *capMap[time.Time]
	flapN   *capMap[int]
	flapAt  *capMap[time.Time]
	flapOut *capMap[time.Time]

	poolUsers, poolWorkers, poolIdle string
	poolRTAt                         string
	poolH1, poolH5, poolH60          string
	poolHRAt                         string
	poolRej                          float64
	poolRejSet                       bool

	pend      string
	pendOff   int64
	pendStamp time.Time
	pendOK    bool
}

// NewEngine returns an engine with fresh state.
func NewEngine(cfg Config) *Engine {
	flap := cfg.FlapSecs
	if flap <= 0 {
		flap = defaultFlapSecs
	}
	n := cfg.MaxTracked
	if n <= 0 {
		n = defaultTracked
	}
	return &Engine{
		flap: flap, noFlap: cfg.NoFlap, groups: cfg.Groups,
		tags: newCapMap[string](n), api: newCapMap[bool](n), authAt: newCapMap[time.Time](n),
		flapN: newCapMap[int](n), flapAt: newCapMap[time.Time](n), flapOut: newCapMap[time.Time](n),
		poolRTAt: "", poolHRAt: "",
	}
}

// Feed consumes one log line (without its newline) that started at byte
// offset off and returns the rows it produced. Long messages the daemon split
// across several lines are rejoined first, so a split message yields its rows
// when the last piece arrives or when Flush is called.
func (e *Engine) Feed(line string, off int64) []Row {
	line = strings.TrimSuffix(line, "\r")
	var rows []Row
	stamp, hasStamp := lineStamp(line)
	if e.pend != "" {
		gap := int64(-1)
		if hasStamp && e.pendOK {
			gap = stamp.Sub(e.pendStamp).Milliseconds()
		}
		if gap >= 0 && gap <= joinGapMs {
			e.pendStamp = stamp
			c := line[stampLen(line)+1:]
			e.pend += c
			if len(c) == joinChunk {
				return nil
			}
			rows = e.process(e.pend, e.pendOff, rows)
			e.pend = ""
			return rows
		}
		rows = e.process(e.pend, e.pendOff, rows)
		e.pend = ""
	}
	if sl := stampLen(line); sl > 0 && len(line)-sl-1 == joinChunk {
		e.pend, e.pendOff, e.pendStamp, e.pendOK = line, off, stamp, hasStamp
		return rows
	}
	return e.process(line, off, rows)
}

// Flush emits a pending split line, if any. Call it when the input goes idle.
func (e *Engine) Flush() []Row {
	if e.pend == "" {
		return nil
	}
	rows := e.process(e.pend, e.pendOff, nil)
	e.pend = ""
	return rows
}

// stampLen is the length of a leading "[...]" stamp including the bracket, or 0.
func stampLen(s string) int {
	if len(s) == 0 || s[0] != '[' {
		return 0
	}
	i := strings.Index(s, "] ")
	if i < 0 {
		return 0
	}
	return i + 1
}

func parseStamp(ts string) (time.Time, bool) {
	if len(ts) < 19 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02 15:04:05", ts[:19])
	if err != nil {
		return time.Time{}, false
	}
	if len(ts) >= 23 && ts[19] == '.' {
		if ms, err := strconv.Atoi(ts[20:23]); err == nil {
			t = t.Add(time.Duration(ms) * time.Millisecond)
		}
	}
	return t, true
}

func lineStamp(line string) (time.Time, bool) {
	sl := stampLen(line)
	if sl == 0 {
		return time.Time{}, false
	}
	return parseStamp(line[1 : sl-1])
}

func validHMS(t string) bool {
	if len(t) < 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		c := t[i]
		if i == 2 || i == 5 {
			if c != ':' {
				return false
			}
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func secsBetween(a, b time.Time) int {
	return int(b.Sub(a) / time.Second)
}

// ctx is the per-line context.
type ctx struct {
	off   int64
	t     time.Time
	valid bool   // a usable timestamp is present
	hms   string // HH:MM:SS as printed
	sec   string // timestamp to the second, "" when absent
}

func (e *Engine) process(line string, off int64, rows []Row) []Row {
	c := ctx{off: off}
	msg := line
	if sl := stampLen(line); sl > 0 {
		ts := line[1 : sl-1]
		msg = line[sl+1:]
		if len(ts) >= 19 {
			c.hms = ts[11:19]
		} else if len(ts) > 11 {
			c.hms = ts[11:]
		}
		if validHMS(c.hms) {
			if t, ok := parseStamp(ts); ok {
				c.t, c.valid = t, true
			}
		}
		if len(ts) >= 19 {
			c.sec = ts[:19]
		}
	}
	if r, ok := e.rule(c, msg); ok {
		rows = append(rows, r)
	}
	return rows
}

func (e *Engine) row(c ctx, st Style, code, text string) Row {
	r := Row{Time: c.t, Offset: c.off, Stamp: c.hms, Style: st, Code: code, AddrColour: -1, Text: text}
	return r
}

func (e *Engine) addrRow(c ctx, addr, text string) Row {
	code, idx := e.addrColor(addr)
	r := e.row(c, StyleAddr, code, text)
	r.AddrColour = idx
	return r
}

// rule applies the ordered rules; ok is false when the line is hidden.
func (e *Engine) rule(c ctx, msg string) (Row, bool) {
	hms := c.hms
	switch {
	case reUser.MatchString(msg):
		return e.userRow(c, msg)
	case strings.HasPrefix(msg, `Pool:{"runtime"`):
		e.poolUsers, e.poolWorkers, e.poolIdle = jget(msg, "Users"), jget(msg, "Workers"), jget(msg, "Idle")
		e.poolRTAt = c.sec
		return Row{}, false
	case strings.HasPrefix(msg, `Pool:{"hashrate1m"`):
		e.poolH1, e.poolH5, e.poolH60 = jget(msg, "hashrate1m"), jget(msg, "hashrate5m"), jget(msg, "hashrate1hr")
		e.poolHRAt = c.sec
		return Row{}, false
	case strings.HasPrefix(msg, `Pool:{"diff"`):
		return e.poolRow(c, msg), true
	case rePassword.MatchString(msg):
		f := strings.Fields(msg)
		e.tags.set(f[1], e.tags.get(f[1])+" d="+f[5]+" (password)")
		return Row{}, false
	case reAgent.MatchString(msg):
		f := strings.Fields(msg)
		cid := ""
		for k := 0; k < len(f)-1; k++ {
			if f[k] == "client" {
				cid = f[k+1]
			}
		}
		cid = strings.TrimSuffix(cid, ",")
		e.tags.set(cid, e.tags.get(cid)+" "+f[0]+" floor "+f[len(f)-1])
		return Row{}, false
	case rePattern.MatchString(msg):
		f := strings.Fields(msg)
		e.tags.set(f[1], e.tags.get(f[1])+" pattern "+f[len(f)-1])
		return Row{}, false
	case reAuth.MatchString(msg):
		return e.authRow(c, msg)
	case strings.HasPrefix(msg, "Dropped client "):
		return e.dropRow(c, msg)
	case strings.HasPrefix(msg, "Stored local workbase"), strings.HasPrefix(msg, "Reaping client"):
		return e.row(c, StyleDim, "2", hms+" "+msg), true
	case reBlockBad.MatchString(msg):
		return e.row(c, StyleBlockBad, "1;31", hms+" * "+msg), true
	case reBlockOK.MatchString(msg):
		return e.row(c, StyleBlockGood, "1;32", hms+" * "+msg), true
	case strings.HasPrefix(msg, "Block hash changed to "):
		return e.row(c, StyleNewBlock, "1;35", hms+" ━━━━ NEW NETWORK BLOCK ━━━━ "+msg[22:]), true
	case strings.HasPrefix(msg, "Network diff set to "):
		return e.row(c, StyleNetDiff, "33", hms+" # network diff "+si(msg[20:])), true
	case strings.HasPrefix(msg, "ZMQ block hash"):
		return Row{}, false
	case reError.MatchString(msg):
		return e.row(c, StyleError, "1;31", hms+" "+msg), true
	}
	text := msg
	if hms != "" {
		text = hms + " " + msg
	}
	return e.row(c, StylePlain, "", text), true
}

func (e *Engine) userRow(c ctx, msg string) (Row, bool) {
	sp := strings.IndexByte(msg, ' ')
	kind := msg[:sp]
	n := msg[len(kind)+1:]
	j := strings.Index(n, ":{")
	name := noprefix(n[:j])
	js := n[j+1:]
	h1, rc := jget(js, "hashrate1m"), jget(js, "rejectedcount")
	if (h1 == "0" || h1 == "") && (rc == "" || awkNum(rc) == 0) {
		return Row{}, false
	}
	a := baseAddr(name)
	row := fmt.Sprintf("%s %s%s  %6s %6s %6s", c.hms, name, e.label(a), h1, jget(js, "hashrate5m"), jget(js, "hashrate1hr"))
	if kind == "User" {
		row += fmt.Sprintf("  %sw", jget(js, "workers"))
	}
	row += "  best " + si(jget(js, "bestshare"))
	if kind == "User" {
		row += luck(jget(js, "luck"))
	}
	if rc != "" && awkNum(rc) > 0 {
		row += "  rej " + rc
	}
	return e.addrRow(c, a, row), true
}

func (e *Engine) poolRow(c ctx, msg string) Row {
	rej := awkNum(jget(msg, "rejected"))
	row := c.hms + " POOL "
	if e.poolHRAt == c.sec {
		row += fmt.Sprintf(" %s / %s / %s", e.poolH1, e.poolH5, e.poolH60)
	} else {
		row += "  (hashrate not logged)"
	}
	if e.poolRTAt == c.sec {
		row += fmt.Sprintf("  users %s  workers %s  idle %s", e.poolUsers, e.poolWorkers, e.poolIdle)
	}
	row += "  SPS " + jget(msg, "SPS1m") + luck(jget(msg, "luck"))
	st, code := StylePool, "36"
	if e.poolRejSet && rej > e.poolRej {
		row += "  rejected +" + si(fmt.Sprint(rej-e.poolRej))
		st, code = StylePoolAlert, "1;31"
	}
	e.poolRej, e.poolRejSet = rej, true
	return e.row(c, st, code, row)
}

func (e *Engine) authRow(c ctx, msg string) (Row, bool) {
	f := fields(msg, 12)
	cid, ip, w, u, d := f[2], f[3], f[5], f[8], f[11]
	t := e.tags.get(cid)
	e.tags.del(cid)
	if isAPIProbe(ip, w) {
		e.api.set(cid, true)
		return Row{}, false
	}
	if c.valid {
		e.authAt.set(cid, c.t)
	} else {
		e.authAt.del(cid)
	}
	wn := noprefix(w)
	key := wn + "@" + ip
	if n, ok := e.flapN.lookup(key); ok && n >= 2 && c.valid {
		if last, ok := e.flapAt.lookup(key); ok && secsBetween(last, c.t) < 60 {
			return Row{}, false
		}
	}
	a := baseAddr(u)
	tag := ""
	if t != "" {
		tag = "  [" + t[1:] + "]"
	}
	return e.row(c, StyleConnect, "1;32", fmt.Sprintf("%s + %s%s  %s  d=%s%s", c.hms, wn, e.label(a), ip, d, tag)), true
}

func (e *Engine) dropRow(c ctx, msg string) (Row, bool) {
	f := fields(msg, 8)
	cid, ip, u, w := f[2], f[3], f[5], f[7]
	if _, ok := e.api.lookup(cid); ok || isAPIProbe(ip, w) {
		e.api.del(cid)
		return Row{}, false
	}
	reason, age := "", -1
	if m := reReason.FindString(msg); m != "" {
		reason = m[8:]
	}
	if m := reAge.FindString(msg); m != "" {
		age = int(awkNum(m[5:]))
	}
	if t, ok := e.authAt.lookup(cid); age < 0 && ok && c.valid {
		age = secsBetween(t, c.t)
	}
	wn := noprefix(w)
	a := baseAddr(u)
	key := wn + "@" + ip
	e.authAt.del(cid)
	if !e.noFlap && age >= 0 && age < e.flap && c.valid {
		if last, ok := e.flapAt.lookup(key); ok && secsBetween(last, c.t) >= 60 {
			e.flapN.set(key, 0)
		}
		n, _ := e.flapN.lookup(key)
		n++
		e.flapN.set(key, n)
		e.flapAt.set(key, c.t)
		if n >= 2 {
			if shown, ok := e.flapOut.lookup(key); !ok || secsBetween(shown, c.t) >= 30 {
				e.flapOut.set(key, c.t)
				rs := ""
				if reason != "" {
					rs = ", " + reason
				}
				return e.row(c, StyleFlap, "1;31", fmt.Sprintf("%s ! FLAP %s%s  %s  %d quick drops (last session %ds%s)", c.hms, wn, e.label(a), ip, n, age, rs)), true
			}
			return Row{}, false
		}
	}
	text := c.hms + " - " + wn + e.label(a) + "  " + ip
	if age >= 0 {
		text += "  (" + dur(age) + ")"
	}
	if reason != "" {
		text += "  " + reason
	}
	return e.row(c, StyleDrop, "31", text), true
}

func isAPIProbe(ip, worker string) bool {
	return ip == "127.0.0.1" && strings.HasSuffix(worker, ".cashstratum-api")
}

// fields splits like awk's default split and pads to n entries.
func fields(s string, n int) []string {
	f := strings.Fields(s)
	for len(f) < n {
		f = append(f, "")
	}
	return f
}

func dur(d int) string {
	switch {
	case d < 60:
		return strconv.Itoa(d) + "s"
	case d < 3600:
		return fmt.Sprintf("%dm%02ds", d/60, d%60)
	}
	return fmt.Sprintf("%dh%02dm", d/3600, d%3600/60)
}

func (e *Engine) label(a string) string {
	if g, ok := e.groups[a]; ok {
		return " [" + g.Label + "]"
	}
	return ""
}

// addrColor returns the SGR code and the palette index (-1 for group colours).
func (e *Engine) addrColor(a string) (string, int) {
	if g, ok := e.groups[a]; ok {
		if g.Colour != "" {
			return "1;" + g.Colour, -1
		}
		return "1", -1
	}
	idx := AddrColourIndex(a)
	return "38;5;" + strconv.Itoa(Palette[idx]), idx
}

// AddrColourIndex is the stable palette index of an address.
func AddrColourIndex(a string) int {
	h := 0
	for _, r := range a {
		ord := 0
		if r >= 32 && r < 127 {
			ord = int(r)
		}
		h = (h*31 + ord) % 9973
	}
	return h % len(Palette)
}

func noprefix(s string) string {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func baseAddr(s string) string {
	s = noprefix(s)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i]
	}
	return s
}
