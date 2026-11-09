// Package tail follows an append-only log by byte offset. Every line is
// reported with the offset it starts at and the inode it came from, so
// callers can order events and resume from a saved Cursor after a restart.
//
// The file is only ever opened read-only. Rotation by rename (the inode
// changes) keeps draining the old file for a grace period, because the
// writer re-opens its log path only about once a minute; truncation in place
// resets the offset to zero. A torn final line is held back until its
// newline arrives.
package tail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

const (
	// DefaultPoll is how often Run checks the file.
	DefaultPoll = time.Second
	// DefaultDrain is how long the previous file keeps being read after a
	// rename rotation.
	DefaultDrain = 65 * time.Second

	headBytes = 4096
	chunkSize = 1 << 20
	maxLine   = 16 << 20
)

// ErrCursorMismatch means a saved cursor no longer describes the file; the
// caller should rescan from the start.
var ErrCursorMismatch = errors.New("tail: cursor does not match file")

// Cursor identifies a position in a specific file. HeadSHA256 is the hash of
// the first HeadLen bytes (at most 4 KiB) and detects a different file that
// happens to reuse the inode.
type Cursor struct {
	Inode      uint64 `json:"inode"`
	Offset     int64  `json:"offset"`
	Size       int64  `json:"size"`
	HeadLen    int64  `json:"head_len"`
	HeadSHA256 string `json:"head_sha256"`
}

// Line is one complete line with the position it started at.
type Line struct {
	Text   string
	Offset int64 // byte offset of the first byte of the line
	End    int64 // offset just past the line's newline
	Inode  uint64
}

// Options tunes a Tailer. Zero values pick the defaults.
type Options struct {
	Poll  time.Duration
	Drain time.Duration
	// Now is the clock used for the drain deadline.
	Now func() time.Time
}

type source struct {
	f      *os.File
	ino    uint64
	offset int64
	size   int64
}

// Tailer reads new lines from one path.
type Tailer struct {
	path string
	opt  Options
	cur  *source
	old  *source
	end  time.Time // when the old file stops being drained
}

// New creates a tailer. With a nil cursor it starts at the beginning of the
// file. With a cursor it resumes there, or returns ErrCursorMismatch when
// the file at path is not the one the cursor was taken from. A missing file
// is not an error; the tailer waits for it.
func New(path string, from *Cursor, opt Options) (*Tailer, error) {
	if opt.Poll <= 0 {
		opt.Poll = DefaultPoll
	}
	if opt.Drain <= 0 {
		opt.Drain = DefaultDrain
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	t := &Tailer{path: path, opt: opt}
	if from == nil {
		return t, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && from.Offset == 0 {
			return t, nil
		}
		return nil, ErrCursorMismatch
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if inodeOf(st) != from.Inode || st.Size() < from.Offset || !headMatches(f, from) {
		f.Close()
		return nil, ErrCursorMismatch
	}
	t.cur = &source{f: f, ino: from.Inode, offset: from.Offset, size: st.Size()}
	return t, nil
}

// Close releases the open files.
func (t *Tailer) Close() {
	if t.cur != nil {
		t.cur.f.Close()
		t.cur = nil
	}
	t.dropOld()
}

// Cursor describes the position in the file currently being followed.
func (t *Tailer) Cursor() Cursor {
	if t.cur == nil {
		return Cursor{}
	}
	c := Cursor{Inode: t.cur.ino, Offset: t.cur.offset, Size: t.cur.size}
	c.HeadLen = min(int64(headBytes), t.cur.offset)
	c.HeadSHA256 = headHash(t.cur.f, c.HeadLen)
	return c
}

// Draining reports whether a rotated-away file is still being read.
func (t *Tailer) Draining() bool { return t.old != nil }

// Poll delivers every complete line that has appeared since the last call.
func (t *Tailer) Poll(fn func(Line)) error {
	if t.old != nil {
		if err := t.read(t.old, fn); err != nil {
			t.dropOld()
		}
		if !t.opt.Now().Before(t.end) {
			// One last read so nothing written just before the deadline is lost.
			_ = t.read(t.old, fn)
			t.dropOld()
		}
	}
	st, err := os.Stat(t.path)
	if err != nil {
		if t.cur == nil {
			return nil
		}
		if os.IsNotExist(err) {
			return t.read(t.cur, fn)
		}
		return err
	}
	ino := inodeOf(st)
	switch {
	case t.cur == nil:
		if err := t.openNew(); err != nil {
			return err
		}
	case ino != t.cur.ino:
		// Finish the old file first, then keep it open for late writes.
		if err := t.read(t.cur, fn); err != nil {
			return err
		}
		t.dropOld()
		t.old, t.end = t.cur, t.opt.Now().Add(t.opt.Drain)
		t.cur = nil
		if err := t.openNew(); err != nil {
			return err
		}
	case st.Size() < t.cur.offset:
		t.cur.offset = 0
	}
	if t.cur == nil {
		return nil
	}
	return t.read(t.cur, fn)
}

// Run polls until ctx is done, sleeping the configured interval when idle.
func (t *Tailer) Run(ctx context.Context, fn func(Line)) error {
	tick := time.NewTicker(t.opt.Poll)
	defer tick.Stop()
	for {
		if err := t.Poll(fn); err != nil && ctx.Err() == nil {
			// Transient errors (permissions blip, file mid-rotation) retry.
			_ = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (t *Tailer) openNew() error {
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	t.cur = &source{f: f, ino: inodeOf(st), size: st.Size()}
	return nil
}

func (t *Tailer) dropOld() {
	if t.old != nil {
		t.old.f.Close()
		t.old = nil
	}
}

// read delivers complete lines from s.offset to the current end of file.
func (t *Tailer) read(s *source, fn func(Line)) error {
	if st, err := s.f.Stat(); err == nil {
		if st.Size() < s.offset {
			s.offset = 0
		}
		s.size = st.Size()
	}
	buf := make([]byte, 0, chunkSize)
	pos := s.offset // file position of buf[0]
	chunk := make([]byte, chunkSize)
	for {
		n, err := s.f.ReadAt(chunk, pos+int64(len(buf)))
		buf = append(buf, chunk[:n]...)
		start := 0
		for {
			i := bytes.IndexByte(buf[start:], '\n')
			if i < 0 {
				break
			}
			raw := buf[start : start+i]
			if len(raw) > 0 && raw[len(raw)-1] == '\r' {
				raw = raw[:len(raw)-1]
			}
			end := pos + int64(start+i+1)
			fn(Line{Text: string(raw), Offset: pos + int64(start), End: end, Inode: s.ino})
			s.offset = end
			start += i + 1
		}
		if start > 0 {
			buf = append(buf[:0], buf[start:]...)
			pos += int64(start)
		}
		if len(buf) > maxLine {
			// A line this long is not a log line; skip past it.
			s.offset = pos + int64(len(buf))
			pos, buf = s.offset, buf[:0]
		}
		if err == io.EOF || (err == nil && n == 0) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func headHash(f *os.File, n int64) string {
	h := sha256.New()
	if n > 0 {
		b := make([]byte, n)
		m, _ := f.ReadAt(b, 0)
		h.Write(b[:m])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func headMatches(f *os.File, c *Cursor) bool {
	return headHash(f, c.HeadLen) == c.HeadSHA256
}

func inodeOf(st os.FileInfo) uint64 {
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(sys.Ino)
	}
	return 0
}
