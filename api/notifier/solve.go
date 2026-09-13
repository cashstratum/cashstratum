// The submitted -> terminal solve_id state machine (contract §5): a
// pool.block.submitted mints a solve_id and holds it open for 120s; the
// next pool.block or pool.block.rejected within that window inherits it;
// past the window (or with no submitted event at all) a terminal event
// still fires, with a fresh id of its own.
package main

import (
	"sync"
	"time"
)

// solveIDWindow is how long the notifier holds an open solve_id after a
// pool.block.submitted before treating it as expired.
const solveIDWindow = 120 * time.Second

// solveTracker holds at most one open solve_id at a time. ckpool logs
// submit/confirm/reject strictly in sequence per solve, so a single slot is
// sufficient here -- concurrent solves (contract §5: "theoretically
// possible") each still get their own id because Open always overwrites,
// and the tracker's job is only to bridge the small ms gap between a
// submitted line and its terminal line, not to correlate arbitrarily many
// in-flight solves.
type solveTracker struct {
	mu      sync.Mutex
	id      string
	openAt  time.Time
	hasOpen bool
	now     func() time.Time
}

// newSolveTracker builds a tracker. now is injectable so tests can control
// the 120s window without sleeping; a nil now defaults to time.Now.
func newSolveTracker(now func() time.Time) *solveTracker {
	if now == nil {
		now = time.Now
	}
	return &solveTracker{now: now}
}

// Open records id as the currently open solve_id and starts a fresh 120s
// window, called when a pool.block.submitted line is parsed.
func (t *solveTracker) Open(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.id = id
	t.openAt = t.now()
	t.hasOpen = true
}

// Take resolves a terminal event's solve_id: if a solve_id is open and
// still within its window, it is returned (ok=true) and the window is
// closed. Otherwise ok=false and the caller must mint a fresh id itself --
// Take never invents one, so it stays a pure "is there an inherited id"
// check.
func (t *solveTracker) Take() (id string, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.hasOpen && t.now().Sub(t.openAt) <= solveIDWindow {
		id, ok = t.id, true
	}
	t.hasOpen = false
	t.id = ""
	return id, ok
}
