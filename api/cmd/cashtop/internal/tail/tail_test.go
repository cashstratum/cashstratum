package tail

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func appendTo(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func poll(t *testing.T, tl *Tailer) []Line {
	t.Helper()
	var out []Line
	if err := tl.Poll(func(l Line) { out = append(out, l) }); err != nil {
		t.Fatal(err)
	}
	return out
}

func texts(ls []Line) []string {
	var o []string
	for _, l := range ls {
		o = append(o, l.Text)
	}
	return o
}

func TestOffsetsAndTornLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	appendTo(t, p, "one\ntwo\nthr")
	tl, _ := New(p, nil, Options{})
	defer tl.Close()
	ls := poll(t, tl)
	if !reflect.DeepEqual(texts(ls), []string{"one", "two"}) {
		t.Fatalf("got %v", texts(ls))
	}
	if ls[0].Offset != 0 || ls[1].Offset != 4 || ls[1].End != 8 {
		t.Fatalf("offsets %+v", ls)
	}
	if len(poll(t, tl)) != 0 {
		t.Fatal("torn line emitted")
	}
	appendTo(t, p, "ee\r\n")
	ls = poll(t, tl)
	if len(ls) != 1 || ls[0].Text != "three" || ls[0].Offset != 8 {
		t.Fatalf("got %+v", ls)
	}
}

func TestResumeFromCursor(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	appendTo(t, p, "a\nb\n")
	tl, _ := New(p, nil, Options{})
	poll(t, tl)
	c := tl.Cursor()
	tl.Close()
	appendTo(t, p, "c\n")
	tl2, err := New(p, &c, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tl2.Close()
	if got := texts(poll(t, tl2)); !reflect.DeepEqual(got, []string{"c"}) {
		t.Fatalf("got %v", got)
	}
}

func TestCursorMismatch(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "log")
	appendTo(t, p, "hello world\nsecond\n")
	tl, _ := New(p, nil, Options{})
	poll(t, tl)
	c := tl.Cursor()
	tl.Close()

	// Same size, different content.
	os.WriteFile(p, []byte("HELLO WORLD\nsecond\n"), 0o644)
	if _, err := New(p, &c, Options{}); err != ErrCursorMismatch {
		t.Fatalf("content change: %v", err)
	}
	// Shorter file.
	os.WriteFile(p, []byte("hi\n"), 0o644)
	if _, err := New(p, &c, Options{}); err != ErrCursorMismatch {
		t.Fatalf("shrink: %v", err)
	}
	// Different inode. Create the replacement while the old file still exists
	// and rename it over the path: deleting first lets Linux hand the freed
	// inode straight back to the new file.
	appendTo(t, p+".new", "hello world\nsecond\nmore\n")
	if err := os.Rename(p+".new", p); err != nil {
		t.Fatal(err)
	}
	if _, err := New(p, &c, Options{}); err != ErrCursorMismatch {
		t.Fatalf("inode: %v", err)
	}
}

func TestRenameRotationDrainsOld(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "log")
	now := time.Unix(1000, 0)
	tl, _ := New(p, nil, Options{Drain: 65 * time.Second, Now: func() time.Time { return now }})
	defer tl.Close()
	appendTo(t, p, "a1\n")
	poll(t, tl)
	old := tl.Cursor().Inode
	appendTo(t, p, "a2\n")
	os.Rename(p, p+".1")
	appendTo(t, p, "b1\n")
	ls := poll(t, tl)
	if !reflect.DeepEqual(texts(ls), []string{"a2", "b1"}) || ls[0].Inode != old || ls[1].Inode == old || ls[1].Offset != 0 {
		t.Fatalf("got %+v", ls)
	}
	if !tl.Draining() {
		t.Fatal("expected draining")
	}
	// The daemon still writes to the old inode for a while.
	appendTo(t, p+".1", "a3\n")
	appendTo(t, p, "b2\n")
	now = now.Add(30 * time.Second)
	if got := texts(poll(t, tl)); !reflect.DeepEqual(got, []string{"a3", "b2"}) {
		t.Fatalf("got %v", got)
	}
	now = now.Add(40 * time.Second)
	appendTo(t, p+".1", "late\n")
	poll(t, tl)
	if tl.Draining() {
		t.Fatal("drain should have ended")
	}
	appendTo(t, p+".1", "ignored\n")
	appendTo(t, p, "b3\n")
	if got := texts(poll(t, tl)); !reflect.DeepEqual(got, []string{"b3"}) {
		t.Fatalf("got %v", got)
	}
}

func TestCopytruncate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	appendTo(t, p, "one\ntwo\nthree\n")
	tl, _ := New(p, nil, Options{})
	defer tl.Close()
	poll(t, tl)
	os.Truncate(p, 0)
	appendTo(t, p, "x\n")
	ls := poll(t, tl)
	if len(ls) != 1 || ls[0].Text != "x" || ls[0].Offset != 0 {
		t.Fatalf("got %+v", ls)
	}
}

func TestMissingFileWaits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	tl, err := New(p, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tl.Close()
	if len(poll(t, tl)) != 0 {
		t.Fatal("unexpected lines")
	}
	appendTo(t, p, "hi\n")
	if got := texts(poll(t, tl)); !reflect.DeepEqual(got, []string{"hi"}) {
		t.Fatalf("got %v", got)
	}
}
