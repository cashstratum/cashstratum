package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

// TestNewLogTailer_ClassifiesAppendedLinesIntoPoolEvents covers the wiring
// between the file tailer and the line classifier: history is skipped,
// unrelated lines are dropped, and a submitted -> confirmed pair arrives as
// typed events sharing one solve_id.
func TestNewLogTailer_ClassifiesAppendedLinesIntoPoolEvents(t *testing.T) {
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

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	lines := "[2026-08-30 23:26:26.900] Stratifier: 25 workers, pool hashrate 1.2TH\n" +
		"[2026-08-30 23:26:26.902] BLOCK ACCEPTED!\n" +
		"[2026-08-30 23:26:26.944] Solved and confirmed block 968432 by addr.rig\n"
	if _, err := f.WriteString(lines); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	f.Close()

	var got []any
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case evt := <-ch:
			got = append(got, evt)
		case <-deadline:
			t.Fatalf("timed out waiting for 2 events, got %d: %+v", len(got), got)
		}
	}

	submitted, ok := got[0].(PoolBlockSubmittedEvent)
	if !ok {
		t.Fatalf("event[0] type = %T, want PoolBlockSubmittedEvent", got[0])
	}
	confirmed, ok := got[1].(PoolBlockEvent)
	if !ok {
		t.Fatalf("event[1] type = %T, want PoolBlockEvent", got[1])
	}
	if confirmed.SolveID != submitted.SolveID {
		t.Fatalf("confirmed.SolveID = %q, want %q", confirmed.SolveID, submitted.SolveID)
	}
	if confirmed.Height != 968432 || confirmed.Username != "addr" {
		t.Fatalf("confirmed = %+v, want height 968432 by addr", confirmed)
	}

	select {
	case evt := <-ch:
		t.Fatalf("got unexpected extra event %+v", evt)
	case <-time.After(200 * time.Millisecond):
	}
}
