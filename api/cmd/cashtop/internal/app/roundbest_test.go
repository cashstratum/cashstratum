package app

import (
	"bytes"
	"log"
	"testing"
	"time"

	"cashstratumapi/cmd/cashtop/internal/index"
	"cashstratumapi/internal/fixturegen"
)

// The pool's best share is a truncated integer while the logged difficulty
// is a float, so a share a fraction above it is the same share. The bound
// applies to the user-line fallback only.
func TestRoundBestToleratesTruncatedPoolBest(t *testing.T) {
	_, dir := tree(t, false)
	var logs bytes.Buffer
	a, err := Open(Config{Root: dir, Memory: true, NoNode: true,
		Now: func() time.Time { return fixturegen.FixtureStart }, Log: log.New(&logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	last := fixturegen.FixtureStart
	v := index.View{Best: []index.BestShare{
		{Address: "worker-a", Diff: 80419206527.0, Time: last.Add(time.Minute)},
	}}
	got := a.roundBest(v, last, 80419206526, 0)
	if len(got) != 1 || got[0].Worker != "worker-a" || got[0].Diff != 80419206527.0 {
		t.Fatalf("share dropped or altered: %+v", got)
	}
	if logs.Len() != 0 {
		t.Fatalf("nothing should be logged as dropped, got %q", logs.String())
	}

	v.Best[0].Diff = 80419206526*1.001 + 10
	if got := a.roundBest(v, last, 80419206526, 0); len(got) != 0 {
		t.Fatalf("a share clearly above the pool best must still be dropped: %+v", got)
	}
}

// With sharelogs, a worker's round maximum may legitimately exceed the pool's
// own round best for the whole round: the daemon raises the pool best from a
// share only when that share also beats the submitting connection's
// per-user-round best, which a block found by someone else does not reset.
// Only the network difficulty bounds a sharelog candidate.
func TestRoundBestSharelogWinsOverStalePoolBest(t *testing.T) {
	_, dir := tree(t, false)
	var logs bytes.Buffer
	a, err := Open(Config{Root: dir, Memory: true, NoNode: true,
		Now: func() time.Time { return fixturegen.FixtureStart }, Log: log.New(&logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	last := fixturegen.FixtureStart
	v := index.View{RoundSeen: true, RoundBest: []index.BestShare{
		{Address: "rental.nice", Diff: 400e9, Time: last.Add(time.Hour)},
		{Address: "finder.rig", Diff: 263e9, Time: last.Add(2 * time.Hour)},
	}}
	got := a.roundBest(v, last, 263e9, 521e9)
	if len(got) != 2 || got[0].Worker != "rental.nice" || got[0].Diff != 400e9 {
		t.Fatalf("sharelog share above a stale pool best dropped: %+v", got)
	}
	if logs.Len() != 0 {
		t.Fatalf("nothing should be logged as dropped, got %q", logs.String())
	}
	v.RoundBest[0].Diff = 521e9
	if got := a.roundBest(v, last, 263e9, 521e9); len(got) != 1 || got[0].Worker != "finder.rig" {
		t.Fatalf("a block-sized share must still be dropped: %+v", got)
	}
}
