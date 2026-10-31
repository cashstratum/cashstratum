// RPC enrichment: turns a bare block hash from ZMQ into a network.block
// event carrying height and block_time, via a single getblockheader call
// (noderpc) against the node RPC endpoint ckconf resolved from ckpool.conf.
//
// Enrichment failure is a normal case, never suppressed: contract §4.1 is
// explicit that a node hiccup must still produce an event with
// enriched:false and null height/block_time, so Laravel can treat it as
// "a block happened, go look it up yourself".
package main

import (
	"context"
	"log"

	"cashstratumapi/internal/noderpc"
)

// enrichNetworkBlock fills Height/BlockTime/Enriched on evt from a
// getblockheader call. On any failure it leaves evt with Enriched=false and
// nil Height/BlockTime and still returns it -- enrichment failure must
// never suppress the event (contract §4.1).
func enrichNetworkBlock(ctx context.Context, client *noderpc.Client, evt NetworkBlockEvent) NetworkBlockEvent {
	result, ok, err := client.GetBlockHeader(ctx, evt.Hash)
	if !ok {
		if err != nil {
			// Deliberately no credentials, no raw RPC body -- just
			// the hash and the failure reason.
			log.Printf("enrich: getblockheader failed for %s (%v), sending unenriched", evt.Hash, err)
		}
		evt.Enriched = false
		evt.Height = nil
		evt.BlockTime = nil
		return evt
	}

	height := result.Height
	blockTime := result.Time
	evt.Height = &height
	evt.BlockTime = &blockTime
	evt.Enriched = true
	return evt
}
