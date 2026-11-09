package app

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"cashstratumapi/internal/fixturegen"
	"cashstratumapi/internal/noderpc"
	"cashstratumapi/internal/poolindex"
	"cashstratumapi/internal/solvecheck"
)

// chainNode is a node whose block at each height has a hash and a coinbase
// script; err makes every call fail.
type chainNode struct {
	hash   map[int64]string
	script map[string]string
	err    error
}

func (n *chainNode) GetBlockHash(_ context.Context, h int64) (string, error) {
	if n.err != nil {
		return "", n.err
	}
	return n.hash[h], nil
}

func (n *chainNode) GetBlockCoinbase(_ context.Context, hash string) (noderpc.BlockCoinbase, error) {
	if n.err != nil {
		return noderpc.BlockCoinbase{}, n.err
	}
	return noderpc.BlockCoinbase{ScriptSig: hex.EncodeToString([]byte(n.script[hash]))}, nil
}

// orphanApp opens an app on the fixture and returns it with the newest
// accepted height, whose recorded hash is set to rec.
func orphanApp(t *testing.T, rec string) (*App, int64) {
	t.Helper()
	_, dir := tree(t, true)
	a, err := Open(Config{Root: dir, Memory: true, NoNode: true,
		Now: func() time.Time { return fixturegen.FixtureStart }, Log: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	var h int64
	for _, b := range a.ix.Snapshot().Blocks {
		if b.Status == poolindex.StatusAccepted {
			h = b.Height
			break
		}
	}
	if h == 0 {
		t.Skip("fixture has no accepted block")
	}
	a.ix.SetBlockHash(h, rec)
	a.owner = solvecheck.Owner{Sig: "/mine/"}
	a.net.Tip = int(h) + 1
	return a, h
}

func blockAt(a *App, h int64) poolindex.Block {
	for _, b := range a.ix.Snapshot().Blocks {
		if b.Height == h {
			return b
		}
	}
	return poolindex.Block{}
}

func TestCheckOrphansCompetitorAtHeight(t *testing.T) {
	a, h := orphanApp(t, "ourhash")
	a.chain = &chainNode{hash: map[int64]string{h: "theirhash"}, script: map[string]string{"theirhash": "/other/"}}
	a.checkOrphans(context.Background(), 10)
	b := blockAt(a, h)
	if b.Status != poolindex.StatusOrphaned {
		t.Fatalf("status = %q, want orphaned", b.Status)
	}
	if b.Hash != "ourhash" {
		t.Fatalf("hash = %q, the competitor's hash replaced ours", b.Hash)
	}
}

func TestEnrichSkipsCompetitorBlock(t *testing.T) {
	a, h := orphanApp(t, "ourhash")
	a.chain = &chainNode{hash: map[int64]string{h: "theirhash"}, script: map[string]string{"theirhash": "/other/"}}
	a.enricher = &rewardNode{calls: map[int64]int{}}
	if err := a.enrich(h); err != nil {
		t.Fatal(err)
	}
	if got := blockAt(a, h).Hash; got != "ourhash" {
		t.Fatalf("hash = %q, want ourhash", got)
	}
	if _, ok := a.rewards[h]; ok {
		t.Fatal("took the competitor block's reward")
	}
}

func TestCheckOrphansOursCorrectsHash(t *testing.T) {
	a, h := orphanApp(t, "provisional")
	a.chain = &chainNode{hash: map[int64]string{h: "realhash"}, script: map[string]string{"realhash": "x/mine/x"}}
	a.checkOrphans(context.Background(), 10)
	b := blockAt(a, h)
	if b.Status != poolindex.StatusAccepted {
		t.Fatalf("status = %q, want accepted", b.Status)
	}
	if b.Hash != "realhash" {
		t.Fatalf("hash = %q, want the chain's", b.Hash)
	}
}

func TestCheckOrphansNodeErrorChangesNothing(t *testing.T) {
	a, h := orphanApp(t, "ourhash")
	a.chain = &chainNode{err: errors.New("down")}
	a.checkOrphans(context.Background(), 10)
	b := blockAt(a, h)
	if b.Status != poolindex.StatusAccepted || b.Hash != "ourhash" {
		t.Fatalf("changed on node error: %q %q", b.Status, b.Hash)
	}
}
