package rpc

import "context"

// HeaderDiff is getblockheader with the difficulty and parent link.
type HeaderDiff struct {
	Hash         string  `json:"hash"`
	Height       int64   `json:"height"`
	Time         int64   `json:"time"`
	Difficulty   float64 `json:"difficulty"`
	PreviousHash string  `json:"previousblockhash"`
}

// BlockHeaderDiff fetches a block header including its difficulty.
func (c *Client) BlockHeaderDiff(ctx context.Context, hash string) (HeaderDiff, error) {
	var v HeaderDiff
	err := c.into(ctx, &v, "getblockheader", hash)
	return v, err
}
