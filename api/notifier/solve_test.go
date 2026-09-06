package main

import (
	"testing"
	"time"
)

// fakeClock is a manually advanced time source for deterministic window
// tests -- no real sleeping.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestSolveTracker_TakeWithinWindowReturnsOpenID(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	tr := newSolveTracker(clock.now)

	tr.Open("solve-1")
	clock.advance(5 * time.Second)

	id, ok := tr.Take()
	if !ok || id != "solve-1" {
		t.Fatalf("Take() = (%q, %v), want (solve-1, true)", id, ok)
	}
}

func TestSolveTracker_TakeAfterWindowExpiresReturnsNotOK(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	tr := newSolveTracker(clock.now)

	tr.Open("solve-1")
	clock.advance(121 * time.Second) // just past the 120s window

	id, ok := tr.Take()
	if ok {
		t.Fatalf("Take() = (%q, true), want ok=false past the 120s window", id)
	}
}

func TestSolveTracker_TakeExactlyAtWindowBoundaryStillOK(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	tr := newSolveTracker(clock.now)

	tr.Open("solve-1")
	clock.advance(120 * time.Second) // exactly the window

	id, ok := tr.Take()
	if !ok || id != "solve-1" {
		t.Fatalf("Take() = (%q, %v), want (solve-1, true) at exactly the window boundary", id, ok)
	}
}

func TestSolveTracker_TakeWithNothingOpenReturnsNotOK(t *testing.T) {
	tr := newSolveTracker(nil)
	if id, ok := tr.Take(); ok {
		t.Fatalf("Take() = (%q, true), want ok=false with no Open() call", id)
	}
}

func TestSolveTracker_TakeConsumesTheWindow(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	tr := newSolveTracker(clock.now)

	tr.Open("solve-1")
	if _, ok := tr.Take(); !ok {
		t.Fatalf("first Take() ok = false, want true")
	}
	if id, ok := tr.Take(); ok {
		t.Fatalf("second Take() = (%q, true), want ok=false -- the window is consumed by the first Take()", id)
	}
}

func TestSolveTracker_OpenOverwritesAnyPriorOpenID(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	tr := newSolveTracker(clock.now)

	tr.Open("solve-1")
	tr.Open("solve-2")

	id, ok := tr.Take()
	if !ok || id != "solve-2" {
		t.Fatalf("Take() = (%q, %v), want (solve-2, true) -- last Open() wins", id, ok)
	}
}
