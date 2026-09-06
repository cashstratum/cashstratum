// ckpool.log tailer: follows the file from EOF (never replays history --
// production's log is megabytes with hundreds of historical lines that must
// not re-fire webhooks), parses the three pool-block line formats ckpool
// writes (contract §4.2-4.4), and emits typed events. Rotation-safe: an
// inode change or an in-place truncation both cause a reopen rather than a
// crash or a silent stall.
package main

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// tailPollInterval is how often the tailer checks for new bytes, rotation,
// or truncation once it has caught up to EOF.
const tailPollInterval = 100 * time.Millisecond

// reasonWindow bounds how long a captured "SUBMIT BLOCK RETURNED" reason
// (src/bitcoin.c:331) stays valid before it can be attached to a later
// "...rejected" line (src/stratifier.c:3839). ckpool logs the two back to
// back within the same submission flow, so anything older than this cannot
// belong to the rejection that follows -- prevents a stale reason from a
// much earlier submission leaking onto an unrelated one.
const reasonWindow = 30 * time.Second

var (
	reBlockAccepted   = regexp.MustCompile(`BLOCK ACCEPTED!`)
	reSolvedConfirmed = regexp.MustCompile(`Solved and confirmed block (\d+) by (\S+)`)
	reBlockRejected   = regexp.MustCompile(`Submitted, but had block (\d+) rejected`)
	reSubmitReturned  = regexp.MustCompile(`SUBMIT BLOCK RETURNED: (.+)`)
)

// LogTailer follows ckpool.log and turns the three pool-block line formats
// into typed events delivered to onEvent.
type LogTailer struct {
	path    string
	onEvent func(event any)
	solves  *solveTracker
	now     func() time.Time

	healthy atomic.Bool

	// havePrevIno/prevIno track the inode of the last file this tailer
	// held open, so openFile can tell a genuinely new file (rotation --
	// start at offset 0) apart from the *same* file being reopened after
	// a transient error (same inode -- seek to EOF, never replay). Before
	// the very first open havePrevIno is false, which also seeks to EOF:
	// startup must never replay history either. Net rule: offset 0 is
	// used only for a file whose inode we have never seen before.
	havePrevIno bool
	prevIno     uint64

	// pendingReason/pendingReasonAt implement the reason-capture window
	// described above. Both are only ever touched from processLine,
	// which runs on the tailer's single goroutine -- no mutex needed.
	pendingReason   string
	pendingReasonAt time.Time
}

// NewLogTailer builds a tailer for the ckpool.log at path. now is
// injectable for tests (defaults to time.Now) and is shared with the
// tailer's solveTracker so a fake clock controls both the solve_id window
// and the reason window from one place.
func NewLogTailer(path string, onEvent func(event any)) *LogTailer {
	return newLogTailerWithClock(path, onEvent, nil)
}

func newLogTailerWithClock(path string, onEvent func(event any), now func() time.Time) *LogTailer {
	if now == nil {
		now = time.Now
	}
	return &LogTailer{
		path:    path,
		onEvent: onEvent,
		solves:  newSolveTracker(now),
		now:     now,
	}
}

// Healthy reports whether the tailer currently has the log file open and
// readable. Read by the future heartbeat sink for notifier.heartbeat's
// log_tail_healthy field.
func (t *LogTailer) Healthy() bool {
	return t.healthy.Load()
}

// Run follows the log until ctx is cancelled. It never returns early on a
// recoverable error -- ENOENT during a rotation's rename dance, EACCES
// blips, and truncation-in-place are all tolerated by retrying rather than
// exiting, since a dead tailer silently loses every pool.block* event.
func (t *LogTailer) Run(ctx context.Context) {
	var (
		file     *os.File
		reader   *bufio.Reader
		ino      uint64
		lastSize int64
	)
	defer func() {
		if file != nil {
			file.Close()
		}
	}()

	sleep := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(tailPollInterval):
			return true
		}
	}

	for {
		if ctx.Err() != nil {
			return
		}

		if file == nil {
			f, i, sz, err := t.openFile()
			if err != nil {
				t.healthy.Store(false)
				log.Printf("tail: %s unreadable (%v), retrying", t.path, err)
				if !sleep() {
					return
				}
				continue
			}
			file = f
			reader = bufio.NewReader(file)
			ino, lastSize = i, sz
			t.healthy.Store(true)
			continue
		}

		line, err := reader.ReadString('\n')
		if err == nil {
			t.processLine(strings.TrimRight(line, "\r\n"))
			continue
		}
		if err != io.EOF {
			log.Printf("tail: read error on %s (%v), reopening", t.path, err)
			file.Close()
			file = nil
			t.healthy.Store(false)
			continue
		}

		// Caught up to EOF (or the file is momentarily unreadable) --
		// check for rotation/truncation before waiting for more bytes.
		st, statErr := os.Stat(t.path)
		if statErr != nil {
			t.healthy.Store(false)
			if !sleep() {
				return
			}
			continue
		}

		if newIno := inodeOf(st); newIno != ino {
			log.Printf("tail: %s rotated, reopening from start of the new file", t.path)
			file.Close()
			file = nil
			continue
		}

		if st.Size() < lastSize {
			log.Printf("tail: %s truncated in place (%d -> %d bytes), resuming from start", t.path, lastSize, st.Size())
			if _, seekErr := file.Seek(0, io.SeekStart); seekErr == nil {
				reader.Reset(file)
				lastSize = 0
			}
			continue
		}

		lastSize = st.Size()
		t.healthy.Store(true)
		if !sleep() {
			return
		}
	}
}

// openFile opens path fresh and decides where to start reading from by
// comparing the newly opened file's inode against the last one this tailer
// held (see havePrevIno/prevIno). Two cases seek to EOF -- never replay:
//   - No previous inode at all (the very first open of this tailer's
//     lifetime): startup must not replay ckpool.log's history.
//   - Same inode as before (the fd was lost to a transient error --
//     read error, or a stat/open blip -- and this reopens the identical
//     file): resuming at EOF accepts the small loss of any lines written
//     during the blip rather than replaying everything already seen or
//     already-dead blocks, which the every-minute schedulers cover anyway.
//
// A genuinely new inode (rotation) is the only case that starts at offset
// 0, which is simply the file's natural starting position after os.Open.
func (t *LogTailer) openFile() (*os.File, uint64, int64, error) {
	file, err := os.Open(t.path)
	if err != nil {
		return nil, 0, 0, err
	}
	st, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, 0, err
	}

	newIno := inodeOf(st)
	sameFileAsBefore := t.havePrevIno && newIno == t.prevIno
	if !t.havePrevIno || sameFileAsBefore {
		if _, err := file.Seek(0, io.SeekEnd); err != nil {
			file.Close()
			return nil, 0, 0, err
		}
	}
	t.havePrevIno = true
	t.prevIno = newIno

	return file, newIno, st.Size(), nil
}

// inodeOf extracts the inode number from a os.FileInfo on platforms that
// populate Sys() with *syscall.Stat_t (linux, darwin -- both without cgo).
// Returns 0 if unavailable, which simply disables the inode-change check
// on an unsupported platform rather than panicking.
func inodeOf(st os.FileInfo) uint64 {
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(sys.Ino)
	}
	return 0
}

// processLine matches one already-timestamp-prefixed ckpool.log line
// against the three pool-block formats (plus the optional rejection-reason
// line) and emits the corresponding event. A line matching none of them is
// silently ignored -- ckpool.log carries plenty of lines this notifier does
// not care about.
func (t *LogTailer) processLine(line string) {
	switch {
	case reBlockAccepted.MatchString(line):
		id := newUUIDv7()
		t.solves.Open(id)
		t.pendingReason = ""
		t.emit(newPoolBlockSubmittedEvent(id))

	case reSolvedConfirmed.MatchString(line):
		m := reSolvedConfirmed.FindStringSubmatch(line)
		height, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			log.Printf("tail: unparseable height in confirm line %q: %v", line, err)
			return
		}
		workername := m[2]
		username, worker := splitWorkerName(workername)
		solveID := t.resolveSolveID()
		t.pendingReason = ""
		t.emit(newPoolBlockEvent(solveID, height, workername, username, worker))

	case reBlockRejected.MatchString(line):
		m := reBlockRejected.FindStringSubmatch(line)
		height, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			log.Printf("tail: unparseable height in reject line %q: %v", line, err)
			return
		}
		reason := t.takeReason()
		solveID := t.resolveSolveID()
		t.emit(newPoolBlockRejectedEvent(solveID, height, reason))

	case reSubmitReturned.MatchString(line):
		m := reSubmitReturned.FindStringSubmatch(line)
		t.pendingReason = m[1]
		t.pendingReasonAt = t.now()
	}
}

// resolveSolveID returns the inherited solve_id from an open
// pool.block.submitted within its window, or mints a fresh one -- a
// terminal event is never suppressed or left without a solve_id just
// because it arrived with no (or an expired) submitted event ahead of it.
func (t *LogTailer) resolveSolveID() string {
	if id, ok := t.solves.Take(); ok {
		return id
	}
	return newUUIDv7()
}

// takeReason returns the most recently captured "SUBMIT BLOCK RETURNED"
// reason if it is still within reasonWindow, consuming it either way so it
// cannot be reused by a later, unrelated rejection.
func (t *LogTailer) takeReason() *string {
	if t.pendingReason == "" {
		return nil
	}
	reason := t.pendingReason
	stale := t.now().Sub(t.pendingReasonAt) > reasonWindow
	t.pendingReason = ""
	if stale {
		return nil
	}
	return &reason
}

// emit hands a built event to onEvent, tolerating a nil callback (useful in
// tests that only want to exercise parsing).
func (t *LogTailer) emit(event any) {
	if t.onEvent != nil {
		t.onEvent(event)
	}
}

// splitWorkerName splits a ckpool workername into username/worker following
// the same separator convention as shortWorkerName in
// api/ckpool_api_server.go:741 (first '.' or '_' wins, since ckpool's own
// authorise() derives the username via strsep(&s, "._")). The notifier has
// no separate username hint the way the API's stats endpoints do, so this
// is the scan-only half of that function. worker is nil when there is no
// separator at all (a miner who authorised with a bare address).
func splitWorkerName(workername string) (username string, worker *string) {
	if i := strings.IndexAny(workername, "._"); i >= 0 {
		w := workername[i+1:]
		return workername[:i], &w
	}
	return workername, nil
}
