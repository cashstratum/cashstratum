// ckpool.log line classification: matches the three pool-block line
// formats ckpool writes (contract §4.2-4.4), plus the optional rejection
// reason line, and turns them into typed events. The classifier knows
// nothing about where lines come from -- a file tailer, a replayed fixture
// or a test feeds it one line at a time.
package poolevents

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ReasonWindow bounds how long a captured "SUBMIT BLOCK RETURNED" reason
// (src/bitcoin.c:331) stays valid before it can be attached to a later
// "...rejected" line (src/stratifier.c:3839). ckpool logs the two back to
// back within the same submission flow, so anything older than this cannot
// belong to the rejection that follows -- prevents a stale reason from a
// much earlier submission leaking onto an unrelated one.
const ReasonWindow = 30 * time.Second

var (
	reBlockAccepted   = regexp.MustCompile(`BLOCK ACCEPTED!`)
	reSolvedConfirmed = regexp.MustCompile(`Solved and confirmed block (\d+) by (\S+)`)
	reBlockRejected   = regexp.MustCompile(`Submitted, but had block (\d+) rejected`)
	reSubmitReturned  = regexp.MustCompile(`SUBMIT BLOCK RETURNED: (.+)`)
)

// Classifier turns ckpool.log lines into PoolBlockSubmittedEvent,
// PoolBlockEvent and PoolBlockRejectedEvent values. It carries the
// solve_id and rejection-reason state between lines, so one Classifier
// must see one log's lines in order. It is not safe for concurrent use.
type Classifier struct {
	solves *SolveTracker
	now    func() time.Time

	// Logf receives diagnostics about lines that match a block format
	// but cannot be parsed. Defaults to log.Printf when nil.
	Logf func(format string, args ...any)

	pendingReason   string
	pendingReasonAt time.Time
}

// NewClassifier builds a classifier. now is injectable for tests (nil
// defaults to time.Now) and is shared with the classifier's SolveTracker
// so a fake clock controls both the solve_id window and the reason window
// from one place.
func NewClassifier(now func() time.Time) *Classifier {
	if now == nil {
		now = time.Now
	}
	return &Classifier{
		solves: NewSolveTracker(now),
		now:    now,
	}
}

// Classify matches one already-timestamp-prefixed ckpool.log line against
// the three pool-block formats (plus the optional rejection-reason line)
// and returns the corresponding event. ok is false for a line matching
// none of them -- ckpool.log carries plenty of lines that are not block
// events -- and for the reason line, which only updates state.
func (c *Classifier) Classify(line string) (event any, ok bool) {
	switch {
	case reBlockAccepted.MatchString(line):
		id := NewUUIDv7()
		c.solves.Open(id)
		c.pendingReason = ""
		return NewPoolBlockSubmittedEvent(id), true

	case reSolvedConfirmed.MatchString(line):
		m := reSolvedConfirmed.FindStringSubmatch(line)
		height, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			c.logf("tail: unparseable height in confirm line %q: %v", line, err)
			return nil, false
		}
		workername := m[2]
		username, worker := SplitWorkerName(workername)
		solveID := c.resolveSolveID()
		c.pendingReason = ""
		return NewPoolBlockEvent(solveID, height, workername, username, worker), true

	case reBlockRejected.MatchString(line):
		m := reBlockRejected.FindStringSubmatch(line)
		height, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			c.logf("tail: unparseable height in reject line %q: %v", line, err)
			return nil, false
		}
		reason := c.takeReason()
		solveID := c.resolveSolveID()
		return NewPoolBlockRejectedEvent(solveID, height, reason), true

	case reSubmitReturned.MatchString(line):
		m := reSubmitReturned.FindStringSubmatch(line)
		c.pendingReason = m[1]
		c.pendingReasonAt = c.now()
	}
	return nil, false
}

func (c *Classifier) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// resolveSolveID returns the inherited solve_id from an open
// pool.block.submitted within its window, or mints a fresh one -- a
// terminal event is never suppressed or left without a solve_id just
// because it arrived with no (or an expired) submitted event ahead of it.
func (c *Classifier) resolveSolveID() string {
	if id, ok := c.solves.Take(); ok {
		return id
	}
	return NewUUIDv7()
}

// takeReason returns the most recently captured "SUBMIT BLOCK RETURNED"
// reason if it is still within ReasonWindow, consuming it either way so it
// cannot be reused by a later, unrelated rejection.
func (c *Classifier) takeReason() *string {
	if c.pendingReason == "" {
		return nil
	}
	reason := c.pendingReason
	stale := c.now().Sub(c.pendingReasonAt) > ReasonWindow
	c.pendingReason = ""
	if stale {
		return nil
	}
	return &reason
}

// SplitWorkerName splits a ckpool workername into username/worker following
// the same separator convention as shortWorkerName in
// api/ckpool_api_server.go (first '.' or '_' wins, since ckpool's own
// authorise() derives the username via strsep(&s, "._")). There is no
// separate username hint here the way the API's stats endpoints have one,
// so this is the scan-only half of that function. worker is nil when there
// is no separator at all (a miner who authorised with a bare address).
func SplitWorkerName(workername string) (username string, worker *string) {
	if i := strings.IndexAny(workername, "._"); i >= 0 {
		w := workername[i+1:]
		return workername[:i], &w
	}
	return workername, nil
}
