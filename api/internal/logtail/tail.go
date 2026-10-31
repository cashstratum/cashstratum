// Package logtail follows a log file from EOF and hands each complete line
// to a callback. It never replays history (production's ckpool.log is
// megabytes with hundreds of historical lines that must not re-fire), and
// it is rotation-safe: an inode change or an in-place truncation both cause
// a reopen rather than a crash or a silent stall. What a line means is up
// to the callback.
package logtail

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// PollInterval is how often the tailer checks for new bytes, rotation,
// or truncation once it has caught up to EOF.
const PollInterval = 100 * time.Millisecond

// Tailer follows one log file and delivers each line, with its trailing
// "\r\n" or "\n" stripped, to onLine. onLine runs on the tailer's own
// goroutine, one line at a time, in file order.
type Tailer struct {
	path   string
	onLine func(line string)

	// Logf receives the tailer's operational messages (unreadable file,
	// rotation, truncation). Defaults to log.Printf when nil. Set it
	// before calling Run.
	Logf func(format string, args ...any)

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
}

// New builds a tailer for the file at path. onLine may be nil, in which
// case lines are read and discarded.
func New(path string, onLine func(line string)) *Tailer {
	return &Tailer{path: path, onLine: onLine}
}

// Healthy reports whether the tailer currently has the log file open and
// readable.
func (t *Tailer) Healthy() bool {
	return t.healthy.Load()
}

// Run follows the log until ctx is cancelled. It never returns early on a
// recoverable error -- ENOENT during a rotation's rename dance, EACCES
// blips, and truncation-in-place are all tolerated by retrying rather than
// exiting, since a dead tailer silently loses every line after it.
func (t *Tailer) Run(ctx context.Context) {
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
		case <-time.After(PollInterval):
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
				t.logf("tail: %s unreadable (%v), retrying", t.path, err)
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
			if t.onLine != nil {
				t.onLine(strings.TrimRight(line, "\r\n"))
			}
			continue
		}
		if err != io.EOF {
			t.logf("tail: read error on %s (%v), reopening", t.path, err)
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
			t.logf("tail: %s rotated, reopening from start of the new file", t.path)
			file.Close()
			file = nil
			continue
		}

		if st.Size() < lastSize {
			t.logf("tail: %s truncated in place (%d -> %d bytes), resuming from start", t.path, lastSize, st.Size())
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

func (t *Tailer) logf(format string, args ...any) {
	if t.Logf != nil {
		t.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// openFile opens path fresh and decides where to start reading from by
// comparing the newly opened file's inode against the last one this tailer
// held (see havePrevIno/prevIno). Two cases seek to EOF -- never replay:
//   - No previous inode at all (the very first open of this tailer's
//     lifetime): startup must not replay the log's history.
//   - Same inode as before (the fd was lost to a transient error --
//     read error, or a stat/open blip -- and this reopens the identical
//     file): resuming at EOF accepts the small loss of any lines written
//     during the blip rather than replaying everything already seen.
//
// A genuinely new inode (rotation) is the only case that starts at offset
// 0, which is simply the file's natural starting position after os.Open.
func (t *Tailer) openFile() (*os.File, uint64, int64, error) {
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
