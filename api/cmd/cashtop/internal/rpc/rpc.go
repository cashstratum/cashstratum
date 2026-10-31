// Package rpc is the read-only node RPC client used by cashtop. Every call
// passes through a fixed allowlist of query methods; anything else is
// refused before a request is built, so no wallet, mining-template or
// mutating method can be issued through it.
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"cashstratumapi/internal/ckconf"
	"cashstratumapi/internal/noderpc"
)

var allowed = map[string]bool{
	"getblockchaininfo":  true,
	"getmininginfo":      true,
	"getmempoolinfo":     true,
	"getnetworkhashps":   true,
	"getbestblockhash":   true,
	"getblockhash":       true,
	"getblockheader":     true,
	"getblock":           true,
	"getrawtransaction":  true,
	"getnetworkinfo":     true,
	"getconnectioncount": true,
}

// Allowed reports whether method is on the read-only allowlist.
func Allowed(method string) bool { return allowed[method] }

// Client is a read-only node RPC client.
type Client struct {
	inner  *noderpc.Client
	secret []string
}

// New builds a client from the node credentials in ckpool.conf. ep may be nil;
// calls then fail with an error.
func New(ep *ckconf.BtcdEndpoint) *Client {
	c := &Client{inner: noderpc.NewWithOptions(ep, 5*time.Second, "cashtop")}
	if ep != nil {
		for _, s := range []string{ep.Auth, ep.Pass} {
			if s != "" {
				c.secret = append(c.secret, s)
			}
		}
	}
	return c
}

// String never includes credentials.
func (c *Client) String() string { return "rpc.Client{redacted}" }

// GoString never includes credentials.
func (c *Client) GoString() string { return c.String() }

func (c *Client) redact(err error) error {
	if err == nil {
		return nil
	}
	orig := err.Error()
	msg := orig
	for _, s := range c.secret {
		msg = strings.ReplaceAll(msg, s, "***")
	}
	if msg == orig {
		// Nothing to hide: keep the chain so callers can errors.Is it.
		return err
	}
	return fmt.Errorf("%s", msg)
}

// Call issues an allowlisted method. Disallowed methods fail before any I/O.
func (c *Client) Call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	if !allowed[method] {
		return nil, fmt.Errorf("rpc method %q is not permitted (read-only client)", method)
	}
	raw, err := c.inner.Call(ctx, method, params)
	if err != nil {
		return nil, c.redact(err)
	}
	return raw, nil
}

func (c *Client) into(ctx context.Context, out any, method string, params ...any) error {
	raw, err := c.Call(ctx, method, params...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s: %w", method, err)
	}
	return nil
}

// ChainInfo is the subset of getblockchaininfo cashtop uses.
type ChainInfo struct {
	Chain                string  `json:"chain"`
	Blocks               int64   `json:"blocks"`
	Headers              int64   `json:"headers"`
	BestBlockHash        string  `json:"bestblockhash"`
	Difficulty           float64 `json:"difficulty"`
	VerificationProgress float64 `json:"verificationprogress"`
	InitialBlockDownload bool    `json:"initialblockdownload"`
	MedianTime           int64   `json:"mediantime"`
	SizeOnDisk           int64   `json:"size_on_disk"`
}

// MiningInfo is the subset of getmininginfo cashtop uses.
type MiningInfo struct {
	Blocks        int64   `json:"blocks"`
	Difficulty    float64 `json:"difficulty"`
	NetworkHashPS float64 `json:"networkhashps"`
	PooledTx      int64   `json:"pooledtx"`
	Chain         string  `json:"chain"`
}

// MempoolInfo is the subset of getmempoolinfo cashtop uses.
type MempoolInfo struct {
	Size       int64   `json:"size"`
	Bytes      int64   `json:"bytes"`
	Usage      int64   `json:"usage"`
	MempoolMin float64 `json:"mempoolminfee"`
}

// Header is the subset of getblockheader cashtop uses.
type Header struct {
	Hash          string `json:"hash"`
	Confirmations int64  `json:"confirmations"`
	Height        int64  `json:"height"`
	Time          int64  `json:"time"`
	PreviousHash  string `json:"previousblockhash"`
}

// Reward is the coinbase payout of a block (subsidy plus fees).
type Reward struct {
	BCH      float64
	Satoshis int64
}

func (c *Client) ChainInfo(ctx context.Context) (ChainInfo, error) {
	var v ChainInfo
	err := c.into(ctx, &v, "getblockchaininfo")
	return v, err
}

func (c *Client) MiningInfo(ctx context.Context) (MiningInfo, error) {
	var v MiningInfo
	err := c.into(ctx, &v, "getmininginfo")
	return v, err
}

func (c *Client) MempoolInfo(ctx context.Context) (MempoolInfo, error) {
	var v MempoolInfo
	err := c.into(ctx, &v, "getmempoolinfo")
	return v, err
}

// NetworkHashPS returns the estimated network hash rate over the last blocks
// blocks (H/s).
func (c *Client) NetworkHashPS(ctx context.Context, blocks int) (float64, error) {
	var v float64
	err := c.into(ctx, &v, "getnetworkhashps", blocks)
	return v, err
}

func (c *Client) BestBlockHash(ctx context.Context) (string, error) {
	var v string
	err := c.into(ctx, &v, "getbestblockhash")
	return v, err
}

func (c *Client) BlockHash(ctx context.Context, height int64) (string, error) {
	var v string
	err := c.into(ctx, &v, "getblockhash", height)
	return v, err
}

func (c *Client) BlockHeader(ctx context.Context, hash string) (Header, error) {
	var v Header
	err := c.into(ctx, &v, "getblockheader", hash)
	return v, err
}

// BlockReward sums the coinbase outputs of the block. It reads the txid list
// (verbosity 1) and then only the coinbase transaction, so the response size
// does not grow with the transaction contents of the block.
func (c *Client) BlockReward(ctx context.Context, hash string) (Reward, error) {
	var blk struct {
		Tx []string `json:"tx"`
	}
	if err := c.into(ctx, &blk, "getblock", hash, 1); err != nil {
		return Reward{}, err
	}
	if len(blk.Tx) == 0 {
		return Reward{}, fmt.Errorf("block %s has no transactions", hash)
	}
	var cb struct {
		Vout []struct {
			Value json.Number `json:"value"`
		} `json:"vout"`
	}
	if err := c.into(ctx, &cb, "getrawtransaction", blk.Tx[0], true, hash); err != nil {
		return Reward{}, err
	}
	var sats int64
	for _, o := range cb.Vout {
		f, err := o.Value.Float64()
		if err != nil {
			return Reward{}, fmt.Errorf("decode coinbase value: %w", err)
		}
		sats += int64(math.Round(f * 1e8))
	}
	return Reward{BCH: float64(sats) / 1e8, Satoshis: sats}, nil
}

// IsOrphaned reports whether the block at height with the given hash is no
// longer on the best chain.
func (c *Client) IsOrphaned(ctx context.Context, height int64, hash string) (bool, error) {
	h, err := c.BlockHeader(ctx, hash)
	if err != nil {
		return false, err
	}
	if h.Confirmations == -1 {
		return true, nil
	}
	got, err := c.BlockHash(ctx, height)
	if err != nil {
		return false, err
	}
	return got != hash, nil
}

// Latency measures one round trip to the node using a cheap query.
func (c *Client) Latency(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	if _, err := c.Call(ctx, "getbestblockhash"); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}
