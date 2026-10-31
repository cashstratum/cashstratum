package poolfmt_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/poolfmt"
)

func TestJoinerSplitSimulationMatchesUnsplit(t *testing.T) {
	gen := func(split bool) []string {
		g := newGen()
		g.SetVerbose(true)
		g.SetSplit(split)
		return g.Simulate(40).Lines
	}
	plain, split := gen(false), gen(true)
	if len(split) <= len(plain) {
		t.Fatalf("split produced %d lines for %d unsplit; nothing was split", len(split), len(plain))
	}
	var j poolfmt.Joiner
	var joined []string
	for i, l := range split {
		for _, r := range j.Feed(strings.TrimSuffix(l, "\n"), int64(i)) {
			joined = append(joined, r.Line)
		}
	}
	for _, r := range j.Flush() {
		joined = append(joined, r.Line)
	}
	if len(joined) != len(plain) {
		for i := range joined {
			if joined[i] != strings.TrimSuffix(plain[i], "\n") {
				t.Fatalf("joined %d lines, want %d; first difference at %d:\n%q\n%q", len(joined), len(plain), i, joined[i], plain[i])
			}
		}
		t.Fatalf("joined %d lines, want %d", len(joined), len(plain))
	}
	for i := range plain {
		a := poolfmt.ParseLineIn(strings.TrimSuffix(plain[i], "\n"), time.UTC)
		b := poolfmt.ParseLineIn(joined[i], time.UTC)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("line %d differs:\n%+v\n%+v", i, a, b)
		}
		if joined[i] != strings.TrimSuffix(plain[i], "\n") {
			t.Fatalf("line %d text differs", i)
		}
	}
	// Without joining, the continuation chunks do not parse as User lines.
	n := 0
	for _, l := range split {
		if poolfmt.ParseLineIn(strings.TrimSuffix(l, "\n"), time.UTC).Kind == poolfmt.KindUnknown {
			n++
		}
	}
	if n == 0 {
		t.Error("expected unjoined chunks to be unparseable")
	}
}

func TestFixturegenSplitLine(t *testing.T) {
	body := strings.Repeat("q", 1100)
	ls := fixturegen.SplitLine("[2026-01-15 00:00:00.000] " + body + "\n")
	if len(ls) != 3 {
		t.Fatalf("got %d pieces", len(ls))
	}
	for i, l := range ls[:2] {
		if len(l)-len("[2026-01-15 00:00:00.000] ")-1 != 510 {
			t.Errorf("piece %d wrong size", i)
		}
	}
	exact := "[2026-01-15 00:00:00.000] " + strings.Repeat("q", 510) + "\n"
	if got := fixturegen.SplitLine(exact); len(got) != 1 || got[0] != exact {
		t.Error("510-byte body must not be split")
	}
}
