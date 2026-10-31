// Package noderpc is a small JSON-RPC client for one btcd/bitcoind node
// endpoint, as resolved from ckpool.conf by ckconf.LoadBtcdRPC. It issues
// read-only calls; credentials are sent as HTTP basic auth and never
// appear in returned errors.
package noderpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cashstratumapi/internal/ckconf"
)

// DefaultTimeout bounds a single RPC call made by a client built with New.
// Short on purpose -- block enrichment runs inline in the hot path between
// "block seen on ZMQ" and "event sent", and a slow or wedged node must not
// stall block awareness.
const DefaultTimeout = 2 * time.Second

// DefaultRequestID is the JSON-RPC request id sent by a client built with
// New. Node operators see it in their RPC logs.
const DefaultRequestID = "blocksniper-notifier"

// maxResponseBytes bounds how much of a response body is read: the node is
// trusted and local, but never read unbounded.
const maxResponseBytes = 1 << 20

// ErrResponseTooLarge is returned when a response body exceeds
// maxResponseBytes, instead of handing a truncated body to the decoder.
var ErrResponseTooLarge = errors.New("node RPC response larger than 1 MB")

// BlockHeader is the sliver of getblockheader's JSON-RPC result used here.
type BlockHeader struct {
	Height int64 `json:"height"`
	Time   int64 `json:"time"`
}

// Client issues JSON-RPC calls against one node endpoint.
type Client struct {
	endpoint  *ckconf.BtcdEndpoint
	http      *http.Client
	timeout   time.Duration
	requestID string
}

// New builds a client for the given endpoint with DefaultTimeout and
// DefaultRequestID. ep may be nil (no node RPC configured in ckpool.conf);
// every call then fails with an error rather than panicking.
func New(ep *ckconf.BtcdEndpoint) *Client {
	return NewWithOptions(ep, DefaultTimeout, DefaultRequestID)
}

// NewWithOptions builds a client with an explicit per-call timeout and
// JSON-RPC request id.
func NewWithOptions(ep *ckconf.BtcdEndpoint, timeout time.Duration, requestID string) *Client {
	return &Client{
		endpoint:  ep,
		http:      &http.Client{Timeout: timeout},
		timeout:   timeout,
		requestID: requestID,
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

// Call issues one JSON-RPC call and returns its raw result. A non-200 HTTP
// status, an RPC-level error, or a null/missing result are all errors. The
// response body is never echoed into an error, so an auth failure cannot
// leak into logs.
func (c *Client) Call(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	if c.endpoint == nil {
		return nil, fmt.Errorf("no node RPC endpoint configured")
	}
	if params == nil {
		params = []any{}
	}

	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "1.0",
		"id":      c.requestID,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL(c.endpoint.URL), bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.endpoint.Auth, c.endpoint.Pass)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node RPC returned HTTP %d", resp.StatusCode)
	}

	return parseResponse(raw)
}

// GetBlockHeader calls getblockheader for hashHex and returns its height
// and block time (header time, Unix seconds). The bool return is whether
// the call succeeded -- on false, err explains why and the caller must
// treat the block as unenriched rather than retrying inline.
func (c *Client) GetBlockHeader(ctx context.Context, hashHex string) (result BlockHeader, ok bool, err error) {
	raw, err := c.Call(ctx, "getblockheader", []any{hashHex})
	if err != nil {
		return BlockHeader{}, false, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return BlockHeader{}, false, err
	}
	return result, true, nil
}

// parseResponse decodes a JSON-RPC response envelope and returns its
// result. Split out as a pure function so tests can exercise it directly
// against canned bytes without an HTTP round trip.
func parseResponse(raw []byte) (json.RawMessage, error) {
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return nil, fmt.Errorf("node RPC returned an error: %s", envelope.Error)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil, fmt.Errorf("node RPC returned no result")
	}
	return envelope.Result, nil
}

// parseGetBlockHeaderResponse decodes a full getblockheader JSON-RPC
// response body into a BlockHeader.
func parseGetBlockHeaderResponse(raw []byte) (BlockHeader, error) {
	result, err := parseResponse(raw)
	if err != nil {
		return BlockHeader{}, err
	}
	var header BlockHeader
	if err := json.Unmarshal(result, &header); err != nil {
		return BlockHeader{}, err
	}
	return header, nil
}
