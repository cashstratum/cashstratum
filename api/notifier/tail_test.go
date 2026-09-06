package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// collectEvents returns an onEvent callback that appends to a slice, plus
// the slice itself -- fine for the single-goroutine processLine-level
// tests in this file (Run()-based tests use a channel instead, below).
func collectEvents() (func(event any), *[]any) {
	var events []any
	return func(event any) {
		events = append(events, event)
	}, &events
}

func newTestTailer(clock *fakeClock) (*LogTailer, *[]any) {
	onEvent, events := collectEvents()
	tailer := newLogTailerWithClock("/dev/null", onEvent, clock.now)
	return tailer, events
}

func TestProcessLine_BlockAccepted(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 900000000, time.UTC)}
	tailer, events := newTestTailer(clock)

	tailer.processLine("[2026-08-30 23:26:26.902] BLOCK ACCEPTED!")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	evt, ok := (*events)[0].(PoolBlockSubmittedEvent)
	if !ok {
		t.Fatalf("event type = %T, want PoolBlockSubmittedEvent", (*events)[0])
	}
	if evt.Event != "pool.block.submitted" {
		t.Errorf("Event = %q, want pool.block.submitted", evt.Event)
	}
	if evt.SolveID == "" || evt.SolveID != evt.EventID {
		t.Errorf("SolveID = %q, EventID = %q -- want equal and non-empty", evt.SolveID, evt.EventID)
	}
}

func TestProcessLine_SolvedAndConfirmed(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 944000000, time.UTC)}
	tailer, events := newTestTailer(clock)

	tailer.processLine("[2026-08-30 23:26:26.944] Solved and confirmed block 968432 by 1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS.testrig")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	evt, ok := (*events)[0].(PoolBlockEvent)
	if !ok {
		t.Fatalf("event type = %T, want PoolBlockEvent", (*events)[0])
	}
	if evt.Event != "pool.block" {
		t.Errorf("Event = %q, want pool.block", evt.Event)
	}
	if evt.Height != 968432 {
		t.Errorf("Height = %d, want 968432", evt.Height)
	}
	if evt.WorkerName != "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS.testrig" {
		t.Errorf("WorkerName = %q, want byte-identical to the log", evt.WorkerName)
	}
	if evt.Username != "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS" {
		t.Errorf("Username = %q, want 1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", evt.Username)
	}
	if evt.Worker == nil || *evt.Worker != "testrig" {
		t.Errorf("Worker = %v, want \"testrig\"", evt.Worker)
	}
}

func TestProcessLine_SubmittedButRejected_NoReason(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 958000000, time.UTC)}
	tailer, events := newTestTailer(clock)

	tailer.processLine("[2026-08-30 23:26:26.958] Submitted, but had block 968432 rejected")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	evt, ok := (*events)[0].(PoolBlockRejectedEvent)
	if !ok {
		t.Fatalf("event type = %T, want PoolBlockRejectedEvent", (*events)[0])
	}
	if evt.Height != 968432 {
		t.Errorf("Height = %d, want 968432", evt.Height)
	}
	if evt.Reason != nil {
		t.Errorf("Reason = %v, want nil (no SUBMIT BLOCK RETURNED line preceded it)", *evt.Reason)
	}
}

func TestProcessLine_SubmitBlockReturned_ThenRejected_CapturesReason(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 950000000, time.UTC)}
	tailer, events := newTestTailer(clock)

	tailer.processLine("[2026-08-30 23:26:26.950] SUBMIT BLOCK RETURNED: inconclusive-not-best-prevblk")
	tailer.processLine("[2026-08-30 23:26:26.958] Submitted, but had block 968432 rejected")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1 (the SUBMIT BLOCK RETURNED line emits nothing on its own)", len(*events))
	}
	evt := (*events)[0].(PoolBlockRejectedEvent)
	if evt.Reason == nil || *evt.Reason != "inconclusive-not-best-prevblk" {
		t.Fatalf("Reason = %v, want \"inconclusive-not-best-prevblk\"", evt.Reason)
	}
}

func TestProcessLine_ReasonExpiresOutsideItsWindow(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 0, time.UTC)}
	tailer, events := newTestTailer(clock)

	tailer.processLine("[2026-08-30 23:26:26.000] SUBMIT BLOCK RETURNED: some-stale-reason")
	clock.advance(31 * time.Second) // past reasonWindow (30s)
	tailer.processLine("[2026-08-30 23:26:57.000] Submitted, but had block 1 rejected")

	evt := (*events)[0].(PoolBlockRejectedEvent)
	if evt.Reason != nil {
		t.Fatalf("Reason = %q, want nil -- the captured reason is 31s stale, past the 30s window", *evt.Reason)
	}
}

func TestProcessLine_UnrelatedLineProducesNothing(t *testing.T) {
	tailer, events := newTestTailer(&fakeClock{t: time.Now()})

	tailer.processLine("[2026-08-30 23:26:26.000] Stratifier: 25 workers, pool hashrate 1.2TH")

	if len(*events) != 0 {
		t.Fatalf("got %d events, want 0 for an unrelated line", len(*events))
	}
}

func TestProcessLine_SolveIDCarriedOntoConfirmWithinWindow(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 900000000, time.UTC)}
	tailer, events := newTestTailer(clock)

	tailer.processLine("[2026-08-30 23:26:26.902] BLOCK ACCEPTED!")
	submitted := (*events)[0].(PoolBlockSubmittedEvent)

	clock.advance(1 * time.Second) // well within the 120s window
	tailer.processLine("[2026-08-30 23:26:27.944] Solved and confirmed block 968432 by addr.rig")
	confirmed := (*events)[1].(PoolBlockEvent)

	if confirmed.SolveID != submitted.SolveID {
		t.Fatalf("confirmed.SolveID = %q, want %q (inherited within the window)", confirmed.SolveID, submitted.SolveID)
	}
}

func TestProcessLine_SolveIDFreshOutsideWindow(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 900000000, time.UTC)}
	tailer, events := newTestTailer(clock)

	tailer.processLine("[2026-08-30 23:26:26.902] BLOCK ACCEPTED!")
	submitted := (*events)[0].(PoolBlockSubmittedEvent)

	clock.advance(121 * time.Second) // past the 120s window
	tailer.processLine("[2026-08-30 23:28:27.944] Solved and confirmed block 968432 by addr.rig")
	confirmed := (*events)[1].(PoolBlockEvent)

	if confirmed.SolveID == submitted.SolveID {
		t.Fatalf("confirmed.SolveID = %q, want a fresh id distinct from the expired %q", confirmed.SolveID, submitted.SolveID)
	}
	if confirmed.SolveID == "" {
		t.Fatalf("confirmed.SolveID is empty, want a freshly minted id even with no open submitted event")
	}
}

func TestProcessLine_ConfirmWithNoPrecedingSubmittedStillEmitsWithFreshID(t *testing.T) {
	tailer, events := newTestTailer(&fakeClock{t: time.Now()})

	tailer.processLine("[2026-08-30 23:26:26.944] Solved and confirmed block 968432 by addr.rig")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	evt := (*events)[0].(PoolBlockEvent)
	if evt.SolveID == "" {
		t.Fatalf("SolveID is empty, want a freshly minted id")
	}
}

func TestSplitWorkerName(t *testing.T) {
	cases := []struct {
		workername   string
		wantUsername string
		wantWorker   *string
	}{
		{"1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", nil},
		{"1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS.testrig", "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", strPtr("testrig")},
		{"1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS_testrig", "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", strPtr("testrig")},
		{"addr.rig.1", "addr", strPtr("rig.1")},
	}

	for _, c := range cases {
		gotUsername, gotWorker := splitWorkerName(c.workername)
		if gotUsername != c.wantUsername {
			t.Errorf("splitWorkerName(%q) username = %q, want %q", c.workername, gotUsername, c.wantUsername)
		}
		if (gotWorker == nil) != (c.wantWorker == nil) {
			t.Errorf("splitWorkerName(%q) worker = %v, want %v", c.workername, gotWorker, c.wantWorker)
			continue
		}
		if gotWorker != nil && *gotWorker != *c.wantWorker {
			t.Errorf("splitWorkerName(%q) worker = %q, want %q", c.workername, *gotWorker, *c.wantWorker)
		}
	}
}

func strPtr(s string) *string { return &s }

// --- Run()-based tests: real files, real rotation, real startup behaviour. ---

func waitForEvents(t *testing.T, ch <-chan any, n int, timeout time.Duration) []any {
	t.Helper()
	var got []any
	deadline := time.After(timeout)
	for len(got) < n {
		select {
		case evt := <-ch:
			got = append(got, evt)
		case <-deadline:
			t.Fatalf("timed out waiting for %d events, got %d: %+v", n, len(got), got)
		}
	}
	return got
}

// waitForHealthy blocks until the tailer reports it has successfully
// opened (and, on the very first open, sought past) the log file --
// avoiding a race where a test writes to the file before the tailer's
// startup seek-to-EOF has happened.
func waitForHealthy(t *testing.T, tailer *LogTailer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !tailer.Healthy() {
		if time.Now().After(deadline) {
			t.Fatalf("tailer never became healthy within %s", timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertNoEventWithin(t *testing.T, ch <-chan any, d time.Duration) {
	t.Helper()
	select {
	case evt := <-ch:
		t.Fatalf("got unexpected event %+v, want none within %s", evt, d)
	case <-time.After(d):
	}
}

func TestLogTailer_Run_DoesNotReplayHistoryOnStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpool.log")
	if err := os.WriteFile(path, []byte("[2026-08-30 23:00:00.000] BLOCK ACCEPTED!\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ch := make(chan any, 8)
	tailer := NewLogTailer(path, func(event any) { ch <- event })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tailer.Run(ctx)
	waitForHealthy(t, tailer, 2*time.Second)

	// The tailer has now opened and sought past the pre-existing line;
	// assert it was never delivered.
	assertNoEventWithin(t, ch, 300*time.Millisecond)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := f.WriteString("[2026-08-30 23:26:26.902] BLOCK ACCEPTED!\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	f.Close()

	events := waitForEvents(t, ch, 1, 2*time.Second)
	if _, ok := events[0].(PoolBlockSubmittedEvent); !ok {
		t.Fatalf("event type = %T, want PoolBlockSubmittedEvent", events[0])
	}
}

func TestLogTailer_Run_SurvivesRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpool.log")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ch := make(chan any, 8)
	tailer := NewLogTailer(path, func(event any) { ch <- event })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tailer.Run(ctx)
	waitForHealthy(t, tailer, 2*time.Second)

	appendLine := func(p, line string) {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			t.Fatalf("OpenFile(%s) error = %v", p, err)
		}
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("WriteString() error = %v", err)
		}
		f.Close()
	}

	appendLine(path, "[2026-08-30 23:26:26.902] BLOCK ACCEPTED!")
	waitForEvents(t, ch, 1, 2*time.Second)

	// Simulate logrotate: rename the current file away, create a fresh
	// one at the same path, write to the new file.
	rotated := filepath.Join(dir, "ckpool.log.1")
	if err := os.Rename(path, rotated); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile(new) error = %v", err)
	}
	appendLine(path, "[2026-08-30 23:27:00.000] BLOCK ACCEPTED!")

	events := waitForEvents(t, ch, 1, 2*time.Second)
	if _, ok := events[0].(PoolBlockSubmittedEvent); !ok {
		t.Fatalf("event type = %T, want PoolBlockSubmittedEvent from the post-rotation file", events[0])
	}
}

// TestOpenFile_ReopenSameInodeSeeksToEOF_NoReplay exercises openFile()'s
// inode comparison directly: a transient error (e.g. Run()'s non-EOF read
// error) closes the fd and reopens the *same* file (no rotation, inode
// unchanged). That reopen must seek to EOF, not offset 0 -- otherwise every
// historical line in ckpool.log replays as a burst of fresh-event_id
// pool.block* events that Laravel cannot dedupe (the exact startup-replay
// trap, just moved to error recovery).
func TestOpenFile_ReopenSameInodeSeeksToEOF_NoReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpool.log")
	preExisting := "[2026-08-30 23:00:00.000] BLOCK ACCEPTED!\n[2026-08-30 23:00:01.000] BLOCK ACCEPTED!\n"
	if err := os.WriteFile(path, []byte(preExisting), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tailer := NewLogTailer(path, nil)

	// First open: no previous inode known yet -- must seek to EOF, same
	// as real startup, and this is what establishes prevIno.
	f1, ino1, _, err := tailer.openFile()
	if err != nil {
		t.Fatalf("openFile() (first) error = %v", err)
	}
	r1 := bufio.NewReader(f1)
	if _, err := r1.ReadString('\n'); err != io.EOF {
		t.Fatalf("first open: ReadString() error = %v, want io.EOF -- history must not be visible from a fresh tailer", err)
	}

	// Simulate the fd being lost to a transient error and reopened while
	// nothing has rotated: same path, same inode.
	f1.Close()
	f2, ino2, _, err := tailer.openFile()
	if err != nil {
		t.Fatalf("openFile() (reopen) error = %v", err)
	}
	defer f2.Close()
	if ino2 != ino1 {
		t.Fatalf("ino2 = %d, ino1 = %d -- want the same inode (no rotation happened in this test)", ino2, ino1)
	}

	r2 := bufio.NewReader(f2)
	if _, err := r2.ReadString('\n'); err != io.EOF {
		t.Fatalf("reopen on the same inode: ReadString() error = %v, want io.EOF -- pre-existing lines must not replay", err)
	}

	// A line appended after the reopen must still be delivered.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := f.WriteString("[2026-08-30 23:00:02.000] BLOCK ACCEPTED!\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	f.Close()

	line, err := r2.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() after append error = %v", err)
	}
	if line != "[2026-08-30 23:00:02.000] BLOCK ACCEPTED!\n" {
		t.Fatalf("ReadString() = %q, want the line appended after the reopen", line)
	}
}
