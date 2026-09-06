// RPC enrichment: turns a bare block hash from ZMQ into a network.block
// event carrying height and block_time, via a single getblockheader call
// against the node RPC endpoint ckconf already resolved from ckpool.conf.
//
// Enrichment failure is a normal case, never suppressed: contract §4.1 is
// explicit that a node hiccup must still produce an event with
// enriched:false and null height/block_time, so Laravel can treat it as
// "a block happened, go look it up yourself".
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cashstratumapi/internal/ckconf"
)

// enrichRPCTimeout bounds a single getblockheader call. Short on purpose --
// this runs inline in the hot path between "block seen on ZMQ" and "event
// sent", and a slow/wedged node must not stall block awareness.
const enrichRPCTimeout = 2 * time.Second

// blockHeaderResult is the sliver of getblockheader's JSON-RPC result this
// notifier needs.
type blockHeaderResult struct {
	Height int64 `json:"height"`
	Time   int64 `json:"time"`
}

// nodeRPCClient issues getblockheader calls against one btcd/bitcoind RPC
// endpoint. Kept tiny and single-purpose rather than reusing a general RPC
// client -- this notifier needs exactly one method.
type nodeRPCClient struct {
	endpoint *ckconf.BtcdEndpoint
	http     *http.Client
}

// newNodeRPCClient builds a client for the given endpoint. ep may be nil
// (no node RPC configured in ckpool.conf) -- callers must treat that as
// "enrichment unavailable" rather than dereferencing it.
func newNodeRPCClient(ep *ckconf.BtcdEndpoint) *nodeRPCClient {
	return &nodeRPCClient{
		endpoint: ep,
		http:     &http.Client{Timeout: enrichRPCTimeout},
	}
}

// rpcURL normalises ep.URL the same way api/ckpool_api_server.go's
// fetchNodeInfo does: ckpool.conf writes a bare "host:port" with no scheme,
// which url.Parse/http would otherwise misread, so prepend "http://" when
// no scheme is present.
func rpcURL(raw string) string {
	if !strings.Contains(raw, "://") {
		return "http://" + raw
	}
	return raw
}

// GetBlockHeader calls getblockheader for hashHex and returns its height
// and block_time (header time, Unix seconds). The bool return is whether
// the call succeeded -- on false, err explains why and the caller must
// treat the block as unenriched rather than retrying inline.
func (c *nodeRPCClient) GetBlockHeader(ctx context.Context, hashHex string) (result blockHeaderResult, ok bool, err error) {
	if c.endpoint == nil {
		return blockHeaderResult{}, false, fmt.Errorf("no node RPC endpoint configured")
	}

	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "1.0",
		"id":      "blocksniper-notifier",
		"method":  "getblockheader",
		"params":  []any{hashHex},
	})
	if err != nil {
		return blockHeaderResult{}, false, err
	}

	ctx, cancel := context.WithTimeout(ctx, enrichRPCTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL(c.endpoint.URL), bytes.NewReader(reqBody))
	if err != nil {
		return blockHeaderResult{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.endpoint.Auth, c.endpoint.Pass)

	resp, err := c.http.Do(req)
	if err != nil {
		return blockHeaderResult{}, false, err
	}
	defer resp.Body.Close()

	// Bounded read: trusted local node, but never unbounded.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return blockHeaderResult{}, false, err
	}
	if resp.StatusCode != http.StatusOK {
		// Never echo the body back -- mirrors fetchNodeInfo's stance
		// that an auth error's body must not leak into logs.
		return blockHeaderResult{}, false, fmt.Errorf("node RPC returned HTTP %d", resp.StatusCode)
	}

	parsed, err := parseGetBlockHeaderResponse(raw)
	if err != nil {
		return blockHeaderResult{}, false, err
	}
	return parsed, true, nil
}

// parseGetBlockHeaderResponse decodes a getblockheader JSON-RPC response
// body. Split out as a pure function so tests can exercise it directly
// against canned bytes without an HTTP round trip.
func parseGetBlockHeaderResponse(raw []byte) (blockHeaderResult, error) {
	var envelope struct {
		Result *blockHeaderResult `json:"result"`
		Error  json.RawMessage    `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return blockHeaderResult{}, err
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return blockHeaderResult{}, fmt.Errorf("node RPC returned an error: %s", envelope.Error)
	}
	if envelope.Result == nil {
		return blockHeaderResult{}, fmt.Errorf("node RPC returned no result")
	}
	return *envelope.Result, nil
}

// enrichNetworkBlock fills Height/BlockTime/Enriched on evt from a
// getblockheader call. On any failure it leaves evt with Enriched=false and
// nil Height/BlockTime and still returns it -- enrichment failure must
// never suppress the event (contract §4.1).
func enrichNetworkBlock(ctx context.Context, client *nodeRPCClient, evt NetworkBlockEvent) NetworkBlockEvent {
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
