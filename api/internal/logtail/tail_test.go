package logtail

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Run()-based tests: real files, real rotation, real startup behaviour. ---

func waitForLines(t *testing.T, ch <-chan string, n int, timeout time.Duration) []string {
	t.Helper()
	var got []string
	deadline := time.After(timeout)
	for len(got) < n {
		select {
		case evt := <-ch:
			got = append(got, evt)
		case <-deadline:
			t.Fatalf("timed out waiting for %d lines, got %d: %+v", n, len(got), got)
		}
	}
	return got
}

// waitForHealthy blocks until the tailer reports it has successfully
// opened (and, on the very first open, sought past) the log file --
// avoiding a race where a test writes to the file before the tailer's
// startup seek-to-EOF has happened.
func waitForHealthy(t *testing.T, tailer *Tailer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !tailer.Healthy() {
		if time.Now().After(deadline) {
			t.Fatalf("tailer never became healthy within %s", timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertNoLineWithin(t *testing.T, ch <-chan string, d time.Duration) {
	t.Helper()
	select {
	case evt := <-ch:
		t.Fatalf("got unexpected line %q, want none within %s", evt, d)
	case <-time.After(d):
	}
}

func TestTailer_Run_DoesNotReplayHistoryOnStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpool.log")
	if err := os.WriteFile(path, []byte("[2026-08-30 23:00:00.000] BLOCK ACCEPTED!\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ch := make(chan string, 8)
	tailer := New(path, func(line string) { ch <- line })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tailer.Run(ctx)
	waitForHealthy(t, tailer, 2*time.Second)

	// The tailer has now opened and sought past the pre-existing line;
	// assert it was never delivered.
	assertNoLineWithin(t, ch, 300*time.Millisecond)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := f.WriteString("[2026-08-30 23:26:26.902] BLOCK ACCEPTED!\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	f.Close()

	lines := waitForLines(t, ch, 1, 2*time.Second)
	if want := "[2026-08-30 23:26:26.902] BLOCK ACCEPTED!"; lines[0] != want {
		t.Fatalf("line = %q, want %q", lines[0], want)
	}
}

func TestTailer_Run_SurvivesRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpool.log")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ch := make(chan string, 8)
	tailer := New(path, func(line string) { ch <- line })

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
	waitForLines(t, ch, 1, 2*time.Second)

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

	lines := waitForLines(t, ch, 1, 2*time.Second)
	if want := "[2026-08-30 23:27:00.000] BLOCK ACCEPTED!"; lines[0] != want {
		t.Fatalf("line = %q, want %q from the post-rotation file", lines[0], want)
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

	tailer := New(path, nil)

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
