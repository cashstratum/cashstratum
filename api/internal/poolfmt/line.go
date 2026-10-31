package poolfmt

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind identifies the family of a log line.
type Kind int

const (
	KindUnknown Kind = iota
	KindAuthorised
	KindDropped
	KindUser
	KindWorker
	KindPool
	KindStoredWorkbase
	KindZMQBlock
	KindNetworkDiff
	KindBlockHashChanged
	KindPossibleSolve
	KindSubmitting       // "Submitting block data!" or "Submitting possible block solve share diff"
	KindBlockAccepted    // "BLOCK ACCEPTED!"
	KindSubmitReturned   // "SUBMIT BLOCK RETURNED: <reason>"
	KindSubmitNoResponse // "SUBMIT BLOCK GOT NO RESPONSE!"
	KindSolved           // "Solved and confirmed block N by W [on P]"
	KindSolvedNoHeight   // "Solved and confirmed block!"
	KindSolveShares      // "Block solved after N shares at X% diff"
	KindBlockRejected    // "Submitted, but had block N rejected"
	KindSV2Accepted      // "Block N solved by W accepted on SV2/JD"
	KindSV2Hash          // "SV2/JD block hash H network_diff D"
	KindSV2Rejected      // "SV2/JD block submit rejected/error height N hash H by W"
	KindRentalDetected   // NiceHash / MiningRigRentals
	KindPatternDiff      // "Client N workername 'W' matched pattern 'P', ..."
	KindPasswordDiff     // "Client N set difficulty to D via password parameter"
	KindReaping          // "Reaping client id N addr idle why for S seconds"
)

var kindNames = [...]string{
	"unknown", "authorised", "dropped", "user", "worker", "pool", "stored-workbase",
	"zmq-block", "network-diff", "block-hash-changed", "possible-solve", "submitting",
	"block-accepted", "submit-returned", "submit-no-response", "solved", "solved-no-height",
	"solve-shares", "block-rejected", "sv2-accepted", "sv2-hash", "sv2-rejected",
	"rental-detected", "pattern-diff", "password-diff", "reaping",
}

func (k Kind) String() string {
	if k < 0 || int(k) >= len(kindNames) {
		return "unknown"
	}
	return kindNames[k]
}

// Event is one parsed log line. Only the fields of its Kind are meaningful.
type Event struct {
	Kind    Kind
	Time    time.Time
	HasTime bool

	ClientID string
	Address  string // client network address, or the pool user/finder address where noted
	User     string
	Worker   string // full worker name, "address.rig"
	Diff     float64

	// Dropped.
	NoUser    bool
	Lazily    bool
	Subclient Opt[int64]
	Reason    Opt[string]
	Errno     Opt[int64]
	Age       Opt[int64]

	// User and Worker lines.
	Name  string
	Stats UserStats
	WStat WorkerStats

	// Pool lines.
	Pool PoolStatus

	// Blocks.
	Height   int64
	Hash     string
	Count    int64   // workbase transactions, endpoint, or share count by Kind
	Percent  float64 // luck at the solve
	Stale    bool
	Protocol string // "SV1" / "SV2"
	Message  string // submit rejection text, rental service, reaping reason, pattern, user agent
	Extra    string
}

var (
	reAuthorised = regexp.MustCompile(`^Authorised client (\S+) (\S+) worker (\S+) as user (\S+) with diff (-?\d+)$`)
	reDropped    = regexp.MustCompile(`^Dropped client (\S+) (\S+) (?:user (\S+)|no user) worker (\S+)(.*)$`)
	reDropSub    = regexp.MustCompile(`^Dropped (\S+) (\S+) with (\d+) subclients(.*)$`)
	reUser       = regexp.MustCompile(`^User (.+?):(\{.*\})$`)
	reWorker     = regexp.MustCompile(`^Worker (.+?):(\{.*\})$`)
	reStored     = regexp.MustCompile(`^Stored (local|remote) workbase with (\d+) transactions$`)
	reZMQ        = regexp.MustCompile(`^ZMQ block hash ([0-9a-f]{64}) from endpoint (\d+)$`)
	reNetDiff    = regexp.MustCompile(`^Network diff set to ([0-9.]+)(?: \(proxy \d+:\d+\))?$`)
	reHashChg    = regexp.MustCompile(`^Block hash changed to ([0-9a-f]{64})$`)
	rePossible   = regexp.MustCompile(`^Possible (stale share )?block solve diff ([0-9.eE+-]+) !$`)
	reSubPoss    = regexp.MustCompile(`^Submitting possible block solve share diff ([0-9.eE+-]+) !$`)
	reSubmitRet  = regexp.MustCompile(`^SUBMIT BLOCK RETURNED: (.+)$`)
	reSolved     = regexp.MustCompile(`^Solved and confirmed block (\d+) by (\S+?)(?: on (\S+))?$`)
	reAfter      = regexp.MustCompile(`^Block solved after (\d+) shares at ([0-9.]+)% diff$`)
	reRejected   = regexp.MustCompile(`^Submitted, but had block (\d+) rejected$`)
	reSV2Acc     = regexp.MustCompile(`^Block (\d+) solved by (\S+) accepted on SV2/JD$`)
	reSV2Hash    = regexp.MustCompile(`^SV2/JD block hash (\S+) network_diff ([0-9.]+)$`)
	reSV2Rej     = regexp.MustCompile(`^SV2/JD block submit rejected/error height (\d+) hash ([0-9a-f]{64}) by (.+)$`)
	reRental     = regexp.MustCompile(`^(NiceHash|MiningRigRentals) detected from useragent '(.*)' for client (\S+), applied difficulty (\d+)$`)
	rePattern    = regexp.MustCompile(`^Client (\S+) workername '(.*)' matched pattern '(.*)', applied difficulty (\d+)$`)
	rePassword   = regexp.MustCompile(`^Client (\S+) set difficulty to (\d+) via password parameter$`)
	reReaping    = regexp.MustCompile(`^Reaping client id (\S+) (\S+) idle (.*) for (\d+) seconds$`)
)

func i64(s string) (int64, bool) {
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}

func f64(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

// ParseLine parses one cashstratum.log line including its timestamp prefix,
// using the host's local time zone. Unprefixed lines and lines of unknown
// families yield KindUnknown with HasTime reflecting the prefix.
func ParseLine(line string) Event { return ParseLineIn(line, nil) }

// ParseLineIn is ParseLine with an explicit time zone for the prefix.
func ParseLineIn(line string, loc *time.Location) Event {
	line = strings.TrimRight(line, "\r\n")
	t, rest, ok := ParsePrefixIn(line, loc)
	if !ok {
		return Event{}
	}
	ev := ParseBody(rest)
	ev.Time, ev.HasTime = t, true
	return ev
}

// ParseBody parses the part of a log line after the timestamp prefix.
func ParseBody(body string) Event {
	body = trimLine(body)
	if body == "" {
		return Event{}
	}
	end := strings.IndexAny(body, " :")
	if end < 0 {
		end = len(body)
	}
	switch body[:end] {
	case "Authorised":
		return parseAuthorised(body)
	case "Dropped":
		return parseDropped(body)
	case "User":
		return parseUser(body)
	case "Worker":
		return parseWorkerLine(body)
	case "Pool":
		return parsePool(body)
	case "Stored":
		if m := reStored.FindStringSubmatch(body); m != nil {
			if n, ok := i64(m[2]); ok {
				return Event{Kind: KindStoredWorkbase, Count: n, Extra: m[1]}
			}
		}
	case "ZMQ":
		if m := reZMQ.FindStringSubmatch(body); m != nil {
			if n, ok := i64(m[2]); ok {
				return Event{Kind: KindZMQBlock, Hash: m[1], Count: n}
			}
		}
	case "Network":
		if m := reNetDiff.FindStringSubmatch(body); m != nil {
			if d, ok := f64(m[1]); ok {
				return Event{Kind: KindNetworkDiff, Diff: d}
			}
		}
	case "Block":
		return parseBlock(body)
	case "Possible":
		if m := rePossible.FindStringSubmatch(body); m != nil {
			if d, ok := f64(m[2]); ok {
				return Event{Kind: KindPossibleSolve, Diff: d, Stale: m[1] != ""}
			}
		}
	case "Submitting":
		if body == "Submitting block data!" {
			return Event{Kind: KindSubmitting}
		}
		if m := reSubPoss.FindStringSubmatch(body); m != nil {
			if d, ok := f64(m[1]); ok {
				return Event{Kind: KindSubmitting, Diff: d, Extra: "share"}
			}
		}
	case "BLOCK":
		if body == "BLOCK ACCEPTED!" {
			return Event{Kind: KindBlockAccepted}
		}
	case "SUBMIT":
		if body == "SUBMIT BLOCK GOT NO RESPONSE!" {
			return Event{Kind: KindSubmitNoResponse}
		}
		if m := reSubmitRet.FindStringSubmatch(body); m != nil {
			return Event{Kind: KindSubmitReturned, Message: m[1]}
		}
	case "Solved":
		if body == "Solved and confirmed block!" {
			return Event{Kind: KindSolvedNoHeight}
		}
		if m := reSolved.FindStringSubmatch(body); m != nil {
			if h, ok := i64(m[1]); ok {
				addr, _ := SplitWorker(m[2])
				return Event{Kind: KindSolved, Height: h, Worker: m[2], Address: addr, Protocol: m[3]}
			}
		}
	case "Submitted,":
		if m := reRejected.FindStringSubmatch(body); m != nil {
			if h, ok := i64(m[1]); ok {
				return Event{Kind: KindBlockRejected, Height: h}
			}
		}
	case "SV2/JD":
		return parseSV2(body)
	case "NiceHash", "MiningRigRentals":
		if m := reRental.FindStringSubmatch(body); m != nil {
			if d, ok := f64(m[4]); ok {
				return Event{Kind: KindRentalDetected, Extra: m[1], Message: m[2], ClientID: m[3], Diff: d}
			}
		}
	case "Client":
		if m := rePattern.FindStringSubmatch(body); m != nil {
			if d, ok := f64(m[4]); ok {
				return Event{Kind: KindPatternDiff, ClientID: m[1], Worker: m[2], Message: m[3], Diff: d}
			}
		}
		if m := rePassword.FindStringSubmatch(body); m != nil {
			if d, ok := f64(m[2]); ok {
				return Event{Kind: KindPasswordDiff, ClientID: m[1], Diff: d}
			}
		}
	case "Reaping":
		if m := reReaping.FindStringSubmatch(body); m != nil {
			if n, ok := i64(m[4]); ok {
				return Event{Kind: KindReaping, ClientID: m[1], Address: m[2], Message: m[3], Count: n}
			}
		}
	}
	return Event{}
}

func parseAuthorised(body string) Event {
	m := reAuthorised.FindStringSubmatch(body)
	if m == nil {
		return Event{}
	}
	d, ok := f64(m[5])
	if !ok {
		return Event{}
	}
	return Event{Kind: KindAuthorised, ClientID: m[1], Address: m[2], Worker: m[3], User: m[4], Diff: d}
}

func parseDropped(body string) Event {
	var tail string
	var ev Event
	if m := reDropped.FindStringSubmatch(body); m != nil {
		ev = Event{Kind: KindDropped, ClientID: m[1], Address: m[2], User: m[3], Worker: m[4], NoUser: m[3] == ""}
		tail = m[5]
	} else if m := reDropSub.FindStringSubmatch(body); m != nil {
		n, ok := i64(m[3])
		if !ok {
			return Event{}
		}
		ev = Event{Kind: KindDropped, ClientID: m[1], Address: m[2], Subclient: some(n)}
		tail = m[4]
	} else {
		return Event{}
	}
	for _, tok := range strings.Fields(tail) {
		switch {
		case tok == "lazily":
			ev.Lazily = true
		case strings.HasPrefix(tok, "reason="):
			ev.Reason = some(tok[len("reason="):])
		case strings.HasPrefix(tok, "errno="):
			if n, ok := i64(tok[len("errno="):]); ok {
				ev.Errno = some(n)
			}
		case strings.HasPrefix(tok, "age="):
			if n, ok := i64(tok[len("age="):]); ok {
				ev.Age = some(n)
			}
		}
	}
	return ev
}

func parseUser(body string) Event {
	m := reUser.FindStringSubmatch(body)
	if m == nil {
		return Event{}
	}
	raw, ok := decodeStats(m[2])
	if !ok {
		return Event{}
	}
	return Event{Kind: KindUser, Name: m[1], Stats: raw.user()}
}

func parseWorkerLine(body string) Event {
	m := reWorker.FindStringSubmatch(body)
	if m == nil {
		return Event{}
	}
	raw, ok := decodeStats(m[2])
	if !ok {
		return Event{}
	}
	w := raw.worker()
	w.Name = m[1]
	return Event{Kind: KindWorker, Name: m[1], WStat: w}
}

func parsePool(body string) Event {
	if !strings.HasPrefix(body, "Pool:") {
		return Event{}
	}
	var ps PoolStatus
	if !poolFrom(body[len("Pool:"):], &ps) {
		return Event{}
	}
	return Event{Kind: KindPool, Pool: ps}
}

func parseBlock(body string) Event {
	if m := reHashChg.FindStringSubmatch(body); m != nil {
		return Event{Kind: KindBlockHashChanged, Hash: m[1]}
	}
	if m := reAfter.FindStringSubmatch(body); m != nil {
		n, ok1 := i64(m[1])
		p, ok2 := f64(m[2])
		if ok1 && ok2 {
			return Event{Kind: KindSolveShares, Count: n, Percent: p}
		}
		return Event{}
	}
	if m := reSV2Acc.FindStringSubmatch(body); m != nil {
		if h, ok := i64(m[1]); ok {
			addr, _ := SplitWorker(m[2])
			return Event{Kind: KindSV2Accepted, Height: h, Worker: m[2], Address: addr, Protocol: "SV2"}
		}
	}
	return Event{}
}

func parseSV2(body string) Event {
	if m := reSV2Hash.FindStringSubmatch(body); m != nil {
		if d, ok := f64(m[2]); ok {
			return Event{Kind: KindSV2Hash, Hash: m[1], Diff: d}
		}
	}
	if m := reSV2Rej.FindStringSubmatch(body); m != nil {
		if h, ok := i64(m[1]); ok {
			return Event{Kind: KindSV2Rejected, Height: h, Hash: m[2], Worker: m[3]}
		}
	}
	return Event{}
}
