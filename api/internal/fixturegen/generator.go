package fixturegen

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// DefaultSeed is the seed used when a caller has no preference.
const DefaultSeed uint64 = 0x5eed_c0ffee

// DropReasons is the set of reason tokens appended to Dropped lines.
var DropReasons = []string{"peer_eof", "peer_reset", "epollhup", "bad_json", "idle_drop", "auth_timeout", "write_err", "other"}

// Generator produces log lines against a fake clock. It is not safe for
// concurrent use.
type Generator struct {
	rng      *rand.Rand
	now      time.Time
	start    time.Time
	nUsers   int
	nWorkers int

	Height      int
	NetworkDiff float64
	nextClient  int
	nextHash    int
	round       float64 // percent of network difficulty accumulated this round
	accepted    int64
	rejected    int64
	disconnects int64
	txCount     int
	split       bool
	verbose     bool
}

// New returns a Generator whose clock starts at start (host-local, no zone).
func New(seed uint64, start time.Time) *Generator {
	return &Generator{
		rng:         rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		now:         start,
		start:       start,
		nUsers:      4,
		nWorkers:    3,
		Height:      900000,
		NetworkDiff: 4.5e12,
		nextClient:  1000,
		nextHash:    1,
		round:       12.5,
		txCount:     2,
	}
}

// SetUsers sets how many users and workers per user the periodic lines cover.
func (g *Generator) SetUsers(users, workersPerUser int) {
	if users > 0 {
		g.nUsers = users
	}
	if workersPerUser > 0 {
		g.nWorkers = workersPerUser
	}
}

// Now returns the fake clock.
func (g *Generator) Now() time.Time { return g.now }

// Advance moves the fake clock forward.
func (g *Generator) Advance(d time.Duration) { g.now = g.now.Add(d) }

// Intn exposes the seeded source for callers that need extra variation.
func (g *Generator) Intn(n int) int { return g.rng.IntN(n) }

// Stamp formats the log prefix for t.
func Stamp(t time.Time) string { return t.Format("[2006-01-02 15:04:05.000] ") }

func (g *Generator) line(format string, a ...any) string {
	return Stamp(g.now) + fmt.Sprintf(format, a...) + "\n"
}

func (g *Generator) freshHash() string {
	h := Hash(g.nextHash)
	g.nextHash++
	return h
}

func (g *Generator) rate(u int) float64 {
	base := 2e12 / float64(u+1)
	return base * (0.8 + 0.4*g.rng.Float64())
}

func (g *Generator) hr(u int) [5]string {
	r := g.rate(u)
	return [5]string{SuffixString(r), SuffixString(r * 0.98), SuffixString(r * 1.01), SuffixString(r * 0.97), SuffixString(r * 1.02)}
}

func f(v float64, prec int) string { return strconv.FormatFloat(v, 'f', prec, 64) }

// Authorised formats an "Authorised client" line.
// Source: stratifier.c "Authorised client %s %s worker %s as user %s with diff %"PRId64.
func (g *Generator) Authorised() string {
	u, w := g.rng.IntN(g.nUsers), g.rng.IntN(g.nWorkers)
	g.nextClient++
	diff := []int{1000, 5000, 10000, 20000, 65536}[g.rng.IntN(5)]
	return g.line("Authorised client %d %s worker %s as user %s with diff %d",
		g.nextClient, IP(g.rng.IntN(60)), FullWorker(u, w), Addr(u), diff)
}

// Dropped formats a "Dropped client" line. With reason it carries the
// " reason=… [errno=…] age=…" suffix; lazily inserts the "lazily" token.
// Source: stratifier.c "Dropped client %s %s user %s worker %s %s%s" where the
// suffix is " reason=%s[ errno=%d] age=%d".
func (g *Generator) Dropped(withReason, lazily bool) string {
	u, w := g.rng.IntN(g.nUsers), g.rng.IntN(g.nWorkers)
	lz := ""
	if lazily {
		lz = "lazily"
	}
	suffix := ""
	if withReason {
		r := DropReasons[g.rng.IntN(len(DropReasons))]
		errno := ""
		if r == "peer_reset" || r == "write_err" {
			errno = fmt.Sprintf(" errno=%d", []int{104, 32}[g.rng.IntN(2)])
		}
		suffix = fmt.Sprintf(" reason=%s%s age=%d", r, errno, 5+g.rng.IntN(86400))
	}
	g.disconnects++
	return g.line("Dropped client %d %s user %s worker %s %s%s",
		1000+g.rng.IntN(g.nextClient-999), IP(g.rng.IntN(60)), Addr(u), FullWorker(u, w), lz, suffix)
}

// User formats the periodic "User <addr>:{…}" line (with lastshare).
// Source: stratifier.c "User %s:%s".
func (g *Generator) User(u int) string {
	h := g.hr(u)
	net := g.NetworkDiff
	rs := int64(net * g.round / 100 / float64(g.nUsers))
	extra := ""
	if g.verbose {
		extra = fmt.Sprintf(`,"lastreject":%d,"lastrejectreason":"Above target","rejectrate1m":"%s","rejectrate5m":"%s","rejectrate1hr":"%s","connects":%d,"disconnects":%d`,
			g.now.Unix()-int64(g.rng.IntN(600)), h[0], h[1], h[2], 40+g.rng.IntN(20), 30+g.rng.IntN(20))
	}
	out := g.line(`User %s:{"hashrate1m":"%s","hashrate5m":"%s","hashrate1hr":"%s","hashrate1d":"%s","hashrate7d":"%s","lastshare":%d,"workers":%d,"shares":%d,"roundshares":%d,"bestshare":%s,"luck":%s,"networkdiff":%s,"bestever":%d,"authorised":%d%s}`,
		Addr(u), h[0], h[1], h[2], h[3], h[4], g.now.Unix()-int64(g.rng.IntN(30)), g.nWorkers,
		rs*3+int64(g.rng.IntN(1000)), rs, f(1e9*g.rng.Float64(), 7), f(g.round/float64(g.nUsers), 2), f(net, 1),
		int64(2e12), g.start.Unix(), extra)
	if g.verbose && len(out)-len(Stamp(g.now))-1 == chunkBody {
		// A whole line of exactly one chunk is indistinguishable from the
		// first piece of a split message; keep the fixture unambiguous.
		out = out[:len(out)-2] + " }\n"
	}
	return out
}

// UserSolve formats the shorter solve-time user line (no lastshare).
func (g *Generator) UserSolve(u int) string {
	h := g.hr(u)
	return g.line(`User %s:{"hashrate1m":"%s","hashrate5m":"%s","hashrate1hr":"%s","hashrate1d":"%s","hashrate7d":"%s","shares":%d,"authorised":%d}`,
		Addr(u), h[0], h[1], h[2], h[3], h[4], 1_000_000+g.rng.IntN(1_000_000), g.start.Unix())
}

// Worker formats a "Worker <name>:{…}" line.
func (g *Generator) Worker(u, w int) string {
	h := g.hr(u)
	return g.line(`Worker %s:{"hashrate1m":"%s","hashrate5m":"%s","hashrate1hr":"%s","hashrate1d":"%s","hashrate7d":"%s","shares":%d,"authorised":%d}`,
		FullWorker(u, w), h[0], h[1], h[2], h[3], h[4], 100_000+g.rng.IntN(100_000), g.start.Unix())
}

func (g *Generator) poolRuntime() (int64, int64, int, int, int, int64) {
	rt := int64(g.now.Sub(g.start) / time.Second)
	workers := g.nUsers * g.nWorkers
	return rt, g.now.Unix(), g.nUsers, workers, g.rng.IntN(3), g.disconnects
}

// PoolStatusJSON returns the three pool.status JSON documents (no newline).
func (g *Generator) PoolStatusJSON() [3]string {
	rt, up, users, workers, idle, disc := g.poolRuntime()
	tot := g.rate(0) * float64(g.nUsers)
	g.round += 0.05 + 0.1*g.rng.Float64()
	g.accepted += int64(g.NetworkDiff * 0.0007)
	g.rejected += int64(g.rng.IntN(200000))
	return [3]string{
		fmt.Sprintf(`{"runtime":%d,"lastupdate":%d,"Users":%d,"Workers":%d,"Idle":%d,"Disconnected":%d}`, rt, up, users, workers, idle, disc),
		fmt.Sprintf(`{"hashrate1m":"%s","hashrate5m":"%s","hashrate15m":"%s","hashrate1hr":"%s","hashrate6hr":"%s","hashrate1d":"%s","hashrate7d":"%s"}`,
			SuffixString(tot), SuffixString(tot*0.99), SuffixString(tot*1.01), SuffixString(tot*0.98), SuffixString(tot*1.03), SuffixString(tot*0.97), SuffixString(tot*1.0)),
		fmt.Sprintf(`{"diff":%s,"luck":%s,"accepted":%d,"rejected":%d,"bestshare":%d,"SPS1m":%s,"SPS5m":%s,"SPS15m":%s,"SPS1h":%s}`,
			f(g.round, 2), f(g.round, 2), g.accepted, g.rejected, int64(g.NetworkDiff*0.4),
			f(120+10*g.rng.Float64(), 2), f(118+10*g.rng.Float64(), 2), f(119+10*g.rng.Float64(), 2), f(121+10*g.rng.Float64(), 2)),
	}
}

// Pool returns the three "Pool:{…}" log lines emitted each statistics cycle.
// Source: stratifier.c "Pool:%s" (keys runtime…, hashrate1m…, diff…).
func (g *Generator) Pool() [3]string {
	j := g.PoolStatusJSON()
	return [3]string{g.line("Pool:%s", j[0]), g.line("Pool:%s", j[1]), g.line("Pool:%s", j[2])}
}

// Workbase formats "Stored local workbase with N transactions".
// Source: stratifier.c "Stored %s workbase with %d transactions".
func (g *Generator) Workbase() string {
	g.txCount += g.rng.IntN(6)
	return g.line("Stored local workbase with %d transactions", g.txCount)
}

// ZMQ formats "ZMQ block hash <hash> from endpoint 0".
// Source: stratifier.c "ZMQ block hash %s from endpoint %d".
func (g *Generator) ZMQ(hash string) string {
	return g.line("ZMQ block hash %s from endpoint %d", hash, 0)
}

// NetworkDiffSet formats "Network diff set to <float, 1 decimal>".
// Source: stratifier.c "Network diff set to %.1f".
func (g *Generator) NetworkDiffSet() string {
	g.NetworkDiff *= 0.98 + 0.04*g.rng.Float64()
	return g.line("Network diff set to %s", f(g.NetworkDiff, 1))
}

// BlockHashChanged formats "Block hash changed to <hash>".
// Source: stratifier.c "Block hash changed to %s".
func (g *Generator) BlockHashChanged(hash string) string {
	return g.line("Block hash changed to %s", hash)
}

// NewTip emits the lines for a block found elsewhere on the network.
func (g *Generator) NewTip() []string {
	h := g.freshHash()
	g.Height++
	g.round = 0
	g.accepted, g.rejected, g.txCount = 0, 0, 2
	out := []string{g.ZMQ(h)}
	g.Advance(time.Millisecond)
	out = append(out, g.NetworkDiffSet(), g.BlockHashChanged(h))
	return out
}

// Tick emits one statistics cycle: a User line per user, then three Pool lines.
func (g *Generator) Tick() []string {
	var out []string
	for u := 0; u < g.nUsers; u++ {
		out = append(out, g.User(u))
	}
	p := g.Pool()
	return append(out, p[0], p[1], p[2])
}

// BlockCluster emits the observed ordering for a block the pool found. The
// "Submitting possible block solve share" line is logged after "Solved and
// confirmed" because the stratifier writes it asynchronously. An empty proto
// omits the " on <proto>" suffix.
func (g *Generator) BlockCluster(height int, finder string, proto string) []string {
	hash := g.freshHash()
	u := 0
	for i := 0; i < g.nUsers; i++ {
		if strings.HasPrefix(finder, Addr(i)) {
			u = i
		}
	}
	diff := g.NetworkDiff * (1 + g.rng.Float64()*0.2)
	shares := g.NetworkDiff * g.round / 100
	on := ""
	if proto != "" {
		on = " on " + proto
	}
	var out []string
	out = append(out, g.line("Possible block solve diff %s !", f(diff, 6)))
	out = append(out, g.line("Submitting block data!"))
	g.Advance(2 * time.Millisecond)
	out = append(out, g.line("BLOCK ACCEPTED!"), g.ZMQ(hash))
	g.Advance(231 * time.Millisecond)
	out = append(out, g.line("Solved and confirmed block %d by %s%s", height, finder, on))
	out = append(out, g.UserSolve(u), g.line("Worker %s:%s", finder, workerJSON(g)))
	out = append(out, g.line("Block solved after %.0f shares at %.1f%% diff", shares, g.round))
	out = append(out, g.line("Submitting possible block solve share diff %s !", f(diff, 6)))
	g.Advance(time.Millisecond)
	g.Height = height + 1
	g.round = 0
	g.accepted, g.rejected, g.txCount = 0, 0, 2
	out = append(out, g.NetworkDiffSet(), g.BlockHashChanged(hash))
	return out
}

func workerJSON(g *Generator) string {
	h := g.hr(0)
	return fmt.Sprintf(`{"hashrate1m":"%s","hashrate5m":"%s","hashrate1hr":"%s","hashrate1d":"%s","hashrate7d":"%s"}`, h[0], h[1], h[2], h[3], h[4])
}

// RejectedCluster emits a submit the node refused: no "Solved and confirmed".
func (g *Generator) RejectedCluster(height int, reason string) []string {
	diff := g.NetworkDiff * (1 + g.rng.Float64()*0.1)
	out := []string{
		g.line("Possible block solve diff %s !", f(diff, 6)),
		g.line("Submitting block data!"),
	}
	g.Advance(3 * time.Millisecond)
	out = append(out,
		g.line("SUBMIT BLOCK RETURNED: %s", reason),
		g.line("Submitted, but had block %d rejected", height),
		g.line("Submitting possible block solve share diff %s !", f(diff, 6)))
	return out
}

// Extra emits the less common single-line families: NiceHash, MiningRigRentals,
// pattern-matched difficulty, password difficulty, SV2 acceptance and the
// no-response submit failure.
func (g *Generator) Extra() []string {
	c := 1000 + g.rng.IntN(g.nextClient-999)
	return []string{
		g.line("NiceHash detected from useragent '%s' for client %d, applied difficulty %d", "NiceHash/1.0.0", c, 500000),
		g.line("MiningRigRentals detected from useragent '%s' for client %d, applied difficulty %d", "MRRNode/1.0", c, 20000),
		g.line("Client %d workername '%s' matched pattern '%s', applied difficulty %d", c, Worker(0, 0), "user1.*", 10000),
		g.line("Client %d set difficulty to %d via password parameter", c, 4096),
		g.line("SUBMIT BLOCK GOT NO RESPONSE!"),
		g.line("Block %d solved by %s accepted on SV2/JD", g.Height, FullWorker(1, 0)),
		g.line("Possible stale share block solve diff %s !", f(g.NetworkDiff, 6)),
		g.line("Solved and confirmed block!"),
	}
}
