package app

import (
	"bytes"
	"log"
	"testing"
	"time"

	"cashstratumapi/cmd/cashtop/internal/index"
	"cashstratumapi/internal/fixturegen"
)

// The pool's best share is a truncated integer while the sharelog difficulty
// is a float, so a share a fraction above it is the same share.
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
	v := index.View{RoundSeen: true, RoundBest: []index.BestShare{
		{Address: "worker-a", Diff: 80419206527.0, Time: last.Add(time.Minute)},
	}}
	got := a.roundBest(v, last, 80419206526, 0)
	if len(got) != 1 || got[0].Worker != "worker-a" || got[0].Diff != 80419206527.0 {
		t.Fatalf("share dropped or altered: %+v", got)
	}
	if logs.Len() != 0 {
		t.Fatalf("nothing should be logged as dropped, got %q", logs.String())
	}

	v.RoundBest[0].Diff = 80419206526*1.001 + 10
	if got := a.roundBest(v, last, 80419206526, 0); len(got) != 0 {
		t.Fatalf("a share clearly above the pool best must still be dropped: %+v", got)
	}
}
